package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/storage"
)

func TestAgentStatus(t *testing.T) {
	a := New(storage.NewMemoryStore(), Config{
		DeviceID:  "dev-1",
		ServerURL: "http://127.0.0.1:37617",
	})

	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/status", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d", rr.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["ui_api_version"] != "1" {
		t.Fatalf("unexpected api version: %#v", body["ui_api_version"])
	}
	if body["device_id"] != "dev-1" {
		t.Fatalf("unexpected device id: %#v", body["device_id"])
	}
	if body["platform"] != string(domain.PlatformWindows) {
		t.Fatalf("unexpected platform: %#v", body["platform"])
	}
}

func TestAgentDoesNotGrantCrossOriginBrowserAccess(t *testing.T) {
	a := New(storage.NewMemoryStore(), Config{DeviceID: "dev-1"})
	req := httptest.NewRequest(http.MethodOptions, "/api/records", nil)
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rr := httptest.NewRecorder()

	a.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected cross-origin access: %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestAgentBatchDeleteCanSkipSensitiveRecords(t *testing.T) {
	store := storage.NewMemoryStore()
	a := New(store, Config{Secret: "secret", DeviceID: "dev-1"})
	ctx := context.Background()

	plain, err := a.service.CaptureText(ctx, "plain text")
	if err != nil {
		t.Fatalf("capture plain: %v", err)
	}
	sensitive, err := a.service.CaptureText(ctx, "p@ssw0rd")
	if err != nil {
		t.Fatalf("capture sensitive: %v", err)
	}
	if _, err := a.service.MarkPassword(ctx, sensitive.ID, "alice@example.com", "mail"); err != nil {
		t.Fatalf("mark sensitive: %v", err)
	}

	body := []byte(`{"ids":["` + plain.ID + `","` + sensitive.ID + `"],"skip_sensitive":true}`)
	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/records/batch-delete", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Count   int `json:"count"`
		Skipped int `json:"skipped"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Count != 1 || response.Skipped != 1 {
		t.Fatalf("unexpected delete response: %+v", response)
	}
	if _, err := store.GetRecord(ctx, plain.ID); err != storage.ErrNotFound {
		t.Fatalf("plain record should be deleted, got err %v", err)
	}
	if _, err := store.GetRecord(ctx, sensitive.ID); err != nil {
		t.Fatalf("sensitive record should remain: %v", err)
	}
}

func TestAgentUpdateRemarkRefreshesRevealCacheAndBroadcasts(t *testing.T) {
	a := New(storage.NewMemoryStore(), Config{Secret: "secret", DeviceID: "dev-1"})
	ctx := context.Background()

	rec, err := a.service.CaptureText(ctx, "p@ssw0rd")
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if _, err := a.service.MarkPassword(ctx, rec.ID, "alice@example.com", "old"); err != nil {
		t.Fatalf("mark password: %v", err)
	}
	revealed, err := a.service.Reveal(ctx, rec.ID)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	a.reveals.set(revealed, 30*time.Minute)
	events, unsubscribe := a.events.Subscribe()
	defer unsubscribe()

	body := []byte(`{"remark":"new private note"}`)
	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/records/"+rec.ID+"/remark", bytes.NewReader(body)))

	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rr.Code, rr.Body.String())
	}
	cached, ok := a.reveals.get(rec.ID)
	if !ok {
		t.Fatal("expected reveal cache entry")
	}
	if cached.Remark != "new private note" {
		t.Fatalf("cached remark = %q", cached.Remark)
	}

	select {
	case event := <-events:
		if event.EventType != domain.EventRecordUpdated {
			t.Fatalf("event type = %q", event.EventType)
		}
		if event.Record == nil || event.Record.ID != rec.ID {
			t.Fatalf("unexpected event record: %+v", event.Record)
		}
		if event.DeviceID != "dev-1" {
			t.Fatalf("event device id = %q", event.DeviceID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update event")
	}
}

func TestAgentListsAndServesImageWithoutEmbeddingItInList(t *testing.T) {
	a := New(storage.NewMemoryStore(), Config{DeviceID: "dev-1"})
	imageData := []byte{0x89, 'P', 'N', 'G', 1, 2, 3, 4}
	rec, err := a.service.CaptureImage(context.Background(), "image/png", imageData)
	if err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRecorder()
	a.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/records", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("list status = %d", list.Code)
	}
	var response struct {
		Records []struct {
			ID          string `json:"id"`
			ContentText string `json:"content_text"`
			ContentType string `json:"content_type"`
			ImageURL    string `json:"image_url"`
		} `json:"records"`
	}
	if err := json.NewDecoder(list.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Records) != 1 || response.Records[0].ContentText != "" || response.Records[0].ContentType != "image" {
		t.Fatalf("unexpected image view: %+v", response.Records)
	}
	if response.Records[0].ImageURL == "" {
		t.Fatal("image URL is missing")
	}

	content := httptest.NewRecorder()
	a.ServeHTTP(content, httptest.NewRequest(http.MethodGet, "/api/records/"+rec.ID+"/content", nil))
	if content.Code != http.StatusOK || content.Header().Get("Content-Type") != "image/png" || !bytes.Equal(content.Body.Bytes(), imageData) {
		t.Fatalf("unexpected image response: status=%d type=%q data=%x", content.Code, content.Header().Get("Content-Type"), content.Body.Bytes())
	}
}
