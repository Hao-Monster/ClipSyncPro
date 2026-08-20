package syncer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/storage"
)

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

const DefaultQueueBatchBytes = 16 << 20

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (c *Client) PushRecord(ctx context.Context, rec domain.ClipboardRecord) (domain.ClipboardRecord, error) {
	wireRecord, err := domain.ClipboardRecordForWire(rec)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	payload, err := json.Marshal(wireRecord)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	return c.pushRecordPayload(ctx, payload)
}

func (c *Client) pushRecordPayload(ctx context.Context, payload []byte) (domain.ClipboardRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/clipboard/records", bytes.NewReader(payload))
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.ClipboardRecord{}, responseError(resp)
	}
	var saved domain.ClipboardRecord
	if err := json.NewDecoder(resp.Body).Decode(&saved); err != nil {
		return domain.ClipboardRecord{}, err
	}
	return saved, nil
}

func (c *Client) DeleteRecord(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/api/v1/clipboard/records/"+id, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	return nil
}

func (c *Client) ListRecords(ctx context.Context, query string) ([]domain.ClipboardRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/clipboard/records"+query, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, responseError(resp)
	}
	var body struct {
		Records []domain.ClipboardRecord `json:"records"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Records, nil
}

func (c *Client) GetRecord(ctx context.Context, id string) (domain.ClipboardRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/clipboard/records/"+url.PathEscape(id), nil)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return domain.ClipboardRecord{}, responseError(resp)
	}
	var record domain.ClipboardRecord
	if err := json.NewDecoder(resp.Body).Decode(&record); err != nil {
		return domain.ClipboardRecord{}, err
	}
	return record, nil
}

func (c *Client) FlushQueue(ctx context.Context, store storage.Store, maxItems, maxBytes int) error {
	if maxBytes <= 0 {
		maxBytes = DefaultQueueBatchBytes
	}
	items, err := store.ListPendingQueue(ctx, maxItems)
	if err != nil {
		return err
	}
	consumedBytes := 0
	for _, item := range items {
		payload, err := queueItemPayload(ctx, store, item)
		if err != nil {
			_ = store.MarkQueueRetry(ctx, item.ID, err.Error())
			continue
		}
		if consumedBytes > 0 && consumedBytes+len(payload) > maxBytes {
			break
		}
		consumedBytes += len(payload)
		if err := c.flushOne(ctx, store, item, payload); err != nil {
			_ = store.MarkQueueRetry(ctx, item.ID, err.Error())
			continue
		}
		if err := store.MarkQueueDone(ctx, item.ID); err != nil {
			return err
		}
	}
	return nil
}

func queueItemPayload(ctx context.Context, store storage.Store, item storage.SyncQueueItem) ([]byte, error) {
	switch item.Action {
	case storage.QueueCreate, storage.QueueUpdate:
		rec, err := store.GetRecord(ctx, item.RecordID)
		if err != nil {
			return nil, err
		}
		wireRecord, err := domain.ClipboardRecordForWire(rec)
		if err != nil {
			return nil, err
		}
		return json.Marshal(wireRecord)
	case storage.QueueDelete:
		return []byte(item.RecordID), nil
	default:
		return nil, fmt.Errorf("unknown queue action %q", item.Action)
	}
}

func (c *Client) flushOne(ctx context.Context, store storage.Store, item storage.SyncQueueItem, payload []byte) error {
	switch item.Action {
	case storage.QueueCreate, storage.QueueUpdate:
		saved, err := c.pushRecordPayload(ctx, payload)
		if err != nil {
			return err
		}
		saved.SyncStatus = domain.SyncSynced
		return store.UpsertRecord(ctx, saved)
	case storage.QueueDelete:
		return c.DeleteRecord(ctx, item.RecordID)
	default:
		return fmt.Errorf("unknown queue action %q", item.Action)
	}
}

func (c *Client) Subscribe(ctx context.Context, ownDeviceID string, onEvent func(domain.SyncEvent) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/ws/v1/sync", nil)
	if err != nil {
		return err
	}
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return responseError(resp)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event domain.SyncEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			continue
		}
		if ownDeviceID != "" && event.DeviceID == ownDeviceID {
			continue
		}
		if err := onEvent(event); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return ctx.Err()
}

func responseError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if len(data) == 0 {
		return fmt.Errorf("server returned %s", resp.Status)
	}
	return errors.New(strings.TrimSpace(string(data)))
}
