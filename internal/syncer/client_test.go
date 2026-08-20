package syncer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/storage"
)

func TestFlushQueueMaterializesLatestRecordWithoutStoredPayload(t *testing.T) {
	store := storage.NewMemoryStore()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	rec, err := domain.NewClipboardRecord("local", "device", domain.PlatformWindows, "old", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRecord(t.Context(), rec); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Enqueue(t.Context(), storage.SyncQueueItem{RecordID: rec.ID, Action: storage.QueueCreate}); err != nil {
		t.Fatal(err)
	}
	rec.ContentText = "latest"
	rec.ContentHash = domain.SHA256Hex(rec.ContentText)
	if err := store.UpsertRecord(t.Context(), rec); err != nil {
		t.Fatal(err)
	}

	var received domain.ClipboardRecord
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
			http.Error(w, "invalid", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(received)
	}))
	defer server.Close()

	if err := NewClient(server.URL).FlushQueue(t.Context(), store, 10, DefaultQueueBatchBytes); err != nil {
		t.Fatal(err)
	}
	if received.ContentText != "latest" {
		t.Fatalf("sent content = %q, want latest", received.ContentText)
	}
	items, err := store.ListPendingQueue(t.Context(), 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("queue not drained: %+v, err=%v", items, err)
	}
}

func TestFlushQueueLimitsBatchBytesWithoutStarvingFirstRecord(t *testing.T) {
	store := storage.NewMemoryStore()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		rec, err := domain.NewClipboardRecord("local", "device", domain.PlatformWindows, strings.Repeat(string(rune('a'+i)), 100), now.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertRecord(t.Context(), rec); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Enqueue(t.Context(), storage.SyncQueueItem{RecordID: rec.ID, Action: storage.QueueCreate}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		var rec domain.ClipboardRecord
		_ = json.NewDecoder(r.Body).Decode(&rec)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rec)
	}))
	defer server.Close()

	if err := NewClient(server.URL).FlushQueue(t.Context(), store, 10, 1); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotRequests := requests
	mu.Unlock()
	if gotRequests != 1 {
		t.Fatalf("request count = %d, want 1", gotRequests)
	}
	items, err := store.ListPendingQueue(t.Context(), 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("remaining queue = %+v, err=%v", items, err)
	}
}

func TestPushRecordConvertsBinaryImageOnlyAtWireBoundary(t *testing.T) {
	imageData := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	rec, err := domain.NewClipboardImageRecord("local", "device", domain.PlatformWindows, "image/png", imageData, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire domain.ClipboardRecord
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
			http.Error(w, "invalid", http.StatusBadRequest)
			return
		}
		if wire.ContentText == "" {
			t.Error("wire image body is missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(wire)
	}))
	defer server.Close()
	if _, err := NewClient(server.URL).PushRecord(t.Context(), rec); err != nil {
		t.Fatal(err)
	}
}
