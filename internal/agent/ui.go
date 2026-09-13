package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"clipsyncpro/internal/app"
	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/storage"
	"clipsyncpro/internal/win32"
)

type revealCache struct {
	mu      sync.RWMutex
	entries map[string]revealEntry
}

type revealEntry struct {
	value   domain.RevealedSensitive
	expires time.Time
}

func newRevealCache() revealCache {
	return revealCache{entries: make(map[string]revealEntry)}
}

func (c *revealCache) set(value domain.RevealedSensitive, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[value.RecordID] = revealEntry{value: value, expires: time.Now().Add(ttl)}
}

func (c *revealCache) get(id string) (domain.RevealedSensitive, bool) {
	c.mu.RLock()
	entry, ok := c.entries[id]
	c.mu.RUnlock()
	if !ok || time.Now().After(entry.expires) {
		if ok {
			c.mu.Lock()
			delete(c.entries, id)
			c.mu.Unlock()
		}
		return domain.RevealedSensitive{}, false
	}
	return entry.value, true
}

func (a *Agent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeAgentCORS(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	switch {
	case path == "/" && r.Method == http.MethodGet:
		a.handleIndex(w, r)
	case path == "/api/status" && r.Method == http.MethodGet:
		a.handleStatus(w, r)
	case path == "/api/events" && r.Method == http.MethodGet:
		a.handleEvents(w, r)
	case path == "/api/clipboard/capture-current" && r.Method == http.MethodPost:
		a.handleCaptureCurrent(w, r)
	case path == "/api/records" && r.Method == http.MethodGet:
		a.handleRecords(w, r)
	case path == "/api/records/batch-delete" && r.Method == http.MethodPost:
		a.handleBatchDelete(w, r)
	case strings.HasPrefix(path, "/api/records/"):
		a.handleRecordAction(w, r, strings.TrimPrefix(path, "/api/records/"))
	default:
		writeAgentJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (a *Agent) handleIndex(w http.ResponseWriter, _ *http.Request) {
	writeAgentCORS(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTemplate.Execute(w, nil)
}

func (a *Agent) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeAgentJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"ui_api_version": "1",
		"device_id":      a.cfg.DeviceID,
		"user_id":        a.cfg.UserID,
		"server_url":     a.cfg.ServerURL,
		"ui_addr":        a.cfg.UIAddr,
		"platform":       domain.PlatformWindows,
	})
}

func (a *Agent) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	writeAgentCORS(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := a.events.Subscribe()
	defer unsubscribe()

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case event := <-ch:
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: sync\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

type recordView struct {
	ID              string                    `json:"id"`
	Summary         string                    `json:"summary"`
	ContentText     string                    `json:"content_text,omitempty"`
	ContentType     domain.ContentType        `json:"content_type"`
	IsSensitive     bool                      `json:"is_sensitive"`
	SensitiveType   domain.SensitiveType      `json:"sensitive_type,omitempty"`
	SourcePlatform  domain.Platform           `json:"source_platform"`
	ClientCreatedAt string                    `json:"client_created_at"`
	DisplayTime     string                    `json:"display_time"`
	CharCount       int                       `json:"char_count"`
	SyncStatus      domain.SyncStatus         `json:"sync_status"`
	ImageURL        string                    `json:"image_url,omitempty"`
	Revealed        *domain.RevealedSensitive `json:"revealed,omitempty"`
}

func (a *Agent) handleRecords(w http.ResponseWriter, r *http.Request) {
	filter, ok := a.filterFromRequest(w, r)
	if !ok {
		return
	}
	records, err := a.service.List(r.Context(), filter)
	if err != nil {
		log.Printf("list records failed: %v", err)
		writeAgentJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "storage unavailable",
			"code":  "storage_unavailable",
		})
		return
	}
	views := make([]recordView, 0, len(records))
	for i, rec := range records {
		view := recordView{
			ID:              rec.ID,
			Summary:         domain.RedactedSummary(rec),
			ContentText:     rec.ContentText,
			ContentType:     rec.ContentType,
			IsSensitive:     rec.IsSensitive,
			SensitiveType:   rec.SensitiveType,
			SourcePlatform:  rec.SourcePlatform,
			ClientCreatedAt: domain.FormatMinute(rec.ClientCreatedAt),
			DisplayTime:     rec.ClientCreatedAt.Local().Format("2006-01-02 15:04"),
			CharCount:       len([]rune(rec.ContentText)),
			SyncStatus:      rec.SyncStatus,
		}
		if rec.ContentType == domain.ContentImage {
			view.ContentText = ""
			view.CharCount = 0
			view.ImageURL = requestBaseURL(r) + "/api/records/" + rec.ID + "/content"
		}
		if rec.IsSensitive {
			if revealed, ok := a.reveals.get(rec.ID); ok {
				view.Revealed = &revealed
				view.Summary = maskedSensitiveSummary(revealed)
				view.CharCount = len([]rune(revealed.Content))
			}
		}
		_ = i
		views = append(views, view)
	}
	writeAgentJSON(w, http.StatusOK, map[string]any{"records": views})
}

func (a *Agent) filterFromRequest(w http.ResponseWriter, r *http.Request) (storage.Filter, bool) {
	q := r.URL.Query()
	filter := storage.Filter{
		Keyword:       strings.TrimSpace(q.Get("keyword")),
		SensitiveOnly: q.Get("sensitive") == "true",
		Limit:         10,
	}
	if rawDate := q.Get("date"); rawDate != "" {
		start, end, err := domain.ParseFilterDate(rawDate)
		if err != nil {
			writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return storage.Filter{}, false
		}
		filter.DateStart = &start
		filter.DateEnd = &end
	}
	return filter, true
}

func (a *Agent) handleRecordAction(w http.ResponseWriter, r *http.Request, tail string) {
	parts := strings.Split(tail, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": "record id is required"})
		return
	}
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	switch {
	case action == "" && r.Method == http.MethodDelete:
		a.handleDelete(w, r, []string{id})
	case action == "reveal" && r.Method == http.MethodPost:
		a.handleReveal(w, r, id)
	case action == "content" && r.Method == http.MethodGet:
		a.handleRecordContent(w, r, id)
	case action == "mark-password" && r.Method == http.MethodPost:
		a.handleMarkPassword(w, r, id)
	case action == "mark-account" && r.Method == http.MethodPost:
		a.handleMarkAccount(w, r, id)
	case action == "remark" && r.Method == http.MethodPost:
		a.handleUpdateRemark(w, r, id)
	case action == "copy" && r.Method == http.MethodPost:
		a.handleCopy(w, r, id)
	case action == "paste" && r.Method == http.MethodPost:
		a.handlePaste(w, r, id)
	default:
		writeAgentJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (a *Agent) handleRecordContent(w http.ResponseWriter, r *http.Request, id string) {
	rec, err := a.store.GetRecord(r.Context(), id)
	if err != nil || rec.ContentType != domain.ContentImage {
		writeAgentJSON(w, http.StatusNotFound, map[string]string{"error": "image not found"})
		return
	}
	mimeType, data, err := domain.DecodeClipboardRecordImage(rec)
	if err != nil {
		writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "image data is invalid"})
		return
	}
	writeAgentCORS(w)
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (a *Agent) handleReveal(w http.ResponseWriter, r *http.Request, id string) {
	revealed, err := a.service.Reveal(r.Context(), id)
	if err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	a.reveals.set(revealed, 30*time.Minute)
	writeAgentJSON(w, http.StatusOK, revealed)
}

func (a *Agent) handleMarkPassword(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	var body struct {
		Account string `json:"account"`
		Remark  string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	rec, err := a.service.MarkPassword(r.Context(), id, body.Account, body.Remark)
	if err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	a.broadcastRecordUpdated(rec)
	writeAgentJSON(w, http.StatusOK, rec)
}

func (a *Agent) handleMarkAccount(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	var body struct {
		Password string `json:"password"`
		Remark   string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	rec, err := a.service.MarkAccount(r.Context(), id, body.Password, body.Remark)
	if err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	a.broadcastRecordUpdated(rec)
	writeAgentJSON(w, http.StatusOK, rec)
}

func (a *Agent) handleUpdateRemark(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	var body struct {
		Remark string `json:"remark"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	rec, err := a.service.UpdateRemark(r.Context(), id, body.Remark)
	if err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if revealed, ok := a.reveals.get(id); ok {
		revealed.Remark = body.Remark
		a.reveals.set(revealed, 30*time.Minute)
	}
	a.broadcastRecordUpdated(rec)
	writeAgentJSON(w, http.StatusOK, rec)
}

func (a *Agent) handleCopy(w http.ResponseWriter, r *http.Request, id string) {
	if rec, err := a.store.GetRecord(r.Context(), id); err == nil && rec.ContentType == domain.ContentImage {
		mimeType, data, err := domain.DecodeClipboardRecordImage(rec)
		if err != nil {
			writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "image data is invalid"})
			return
		}
		if err := a.clipboardWrites.write(func() error { return win32.WriteClipboardImage(mimeType, data) }); err != nil {
			writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "copy failed"})
			return
		}
		writeAgentJSON(w, http.StatusOK, map[string]string{"status": "copied"})
		return
	}
	text, err := a.textForAction(r, id)
	if err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := a.clipboardWrites.write(func() error { return win32.WriteClipboardText(text) }); err != nil {
		writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "copy failed"})
		return
	}
	writeAgentJSON(w, http.StatusOK, map[string]string{"status": "copied"})
}

func (a *Agent) handlePaste(w http.ResponseWriter, r *http.Request, id string) {
	if rec, err := a.store.GetRecord(r.Context(), id); err == nil && rec.ContentType == domain.ContentImage {
		mimeType, data, err := domain.DecodeClipboardRecordImage(rec)
		if err != nil {
			writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "image data is invalid"})
			return
		}
		if err := a.clipboardWrites.write(func() error { return win32.PasteImage(mimeType, data) }); err != nil {
			writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "paste failed"})
			return
		}
		writeAgentJSON(w, http.StatusOK, map[string]string{"status": "pasted"})
		return
	}
	text, err := a.textForAction(r, id)
	if err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := a.clipboardWrites.write(func() error { return win32.PasteText(text) }); err != nil {
		writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "paste failed"})
		return
	}
	writeAgentJSON(w, http.StatusOK, map[string]string{"status": "pasted"})
}

func (a *Agent) textForAction(r *http.Request, id string) (string, error) {
	defer r.Body.Close()
	var body struct {
		Field string `json:"field"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Field == "" {
		body.Field = "content"
	}
	rec, err := a.store.GetRecord(r.Context(), id)
	if err != nil {
		return "", err
	}
	if !rec.IsSensitive {
		return rec.ContentText, nil
	}
	revealed, ok := a.reveals.get(id)
	if !ok {
		return "", app.ErrRevealRequired
	}
	switch body.Field {
	case "content":
		return revealed.Content, nil
	case "account":
		return revealed.Account, nil
	case "password":
		return revealed.Password, nil
	case "remark":
		return revealed.Remark, nil
	default:
		return "", errors.New("unknown field")
	}
}

func (a *Agent) handleBatchDelete(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		IDs           []string `json:"ids"`
		SkipSensitive bool     `json:"skip_sensitive"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	ids := body.IDs
	skipped := 0
	if body.SkipSensitive {
		ids = make([]string, 0, len(body.IDs))
		for _, id := range body.IDs {
			rec, err := a.store.GetRecord(r.Context(), id)
			if err == nil && rec.IsSensitive {
				skipped++
				continue
			}
			ids = append(ids, id)
		}
	}
	a.handleDeleteWithSkipped(w, r, ids, skipped)
}

func (a *Agent) handleDelete(w http.ResponseWriter, r *http.Request, ids []string) {
	a.handleDeleteWithSkipped(w, r, ids, 0)
}

func (a *Agent) handleDeleteWithSkipped(w http.ResponseWriter, r *http.Request, ids []string, skipped int) {
	if err := a.service.Delete(r.Context(), ids); err != nil {
		writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete failed"})
		return
	}
	now := time.Now().UTC()
	for _, id := range ids {
		a.broadcastLocalEvent(domain.SyncEvent{
			EventType: domain.EventRecordDeleted,
			RecordID:  id,
			Occurred:  now,
			DeviceID:  a.cfg.DeviceID,
		})
	}
	writeAgentJSON(w, http.StatusOK, map[string]any{"status": "deleted", "count": len(ids), "skipped": skipped})
}

func (a *Agent) handleCaptureCurrent(w http.ResponseWriter, r *http.Request) {
	snapshot, err := win32.ReadClipboard()
	if err != nil {
		writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": "clipboard read failed"})
		return
	}
	if !snapshot.HasText && !snapshot.HasImage {
		writeAgentJSON(w, http.StatusBadRequest, map[string]string{"error": "clipboard has no supported content"})
		return
	}
	rec, err := a.captureSnapshot(r.Context(), snapshot)
	if err != nil {
		writeAgentJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	a.broadcastLocalEvent(domain.SyncEvent{
		EventType: domain.EventRecordCreated,
		RecordID:  rec.ID,
		Occurred:  time.Now().UTC(),
		DeviceID:  a.cfg.DeviceID,
	})
	writeAgentJSON(w, http.StatusOK, rec)
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (a *Agent) broadcastRecordUpdated(rec domain.ClipboardRecord) {
	a.broadcastLocalEvent(domain.SyncEvent{
		EventType: domain.EventRecordUpdated,
		RecordID:  rec.ID,
		Occurred:  time.Now().UTC(),
		DeviceID:  a.cfg.DeviceID,
	})
}

func maskedSensitiveSummary(revealed domain.RevealedSensitive) string {
	account := revealed.Account
	if len([]rune(account)) > 24 {
		runes := []rune(account)
		account = string(runes[:24]) + "..."
	}
	remark := revealed.Remark
	if remark != "" {
		return account + " / " + remark
	}
	if account != "" {
		return account
	}
	return "已解密"
}

func writeAgentJSON(w http.ResponseWriter, status int, value any) {
	writeAgentCORS(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAgentCORS(w http.ResponseWriter) {
	// The WinUI client uses HttpClient and the debug page is same-origin. Do not
	// grant browser origins access to private clipboard records on localhost.
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

var indexTemplate = template.Must(template.New("index").Parse(indexHTML))
