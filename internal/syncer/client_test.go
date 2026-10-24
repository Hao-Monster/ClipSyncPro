package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"clipsyncpro/internal/app"
	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/httpapi"
	"clipsyncpro/internal/storage"
)

func TestFavoriteToggleSyncsThroughDurableQueue(t *testing.T) {
	local := storage.NewMemoryStore()
	remote := storage.NewMemoryStore()
	server := httptest.NewServer(httpapi.NewServer(remote, ""))
	defer server.Close()

	service := app.NewService(local, app.Config{UserID: "local", DeviceID: "dev-1", Platform: domain.PlatformWindows})
	rec, err := service.CaptureText(t.Context(), "keep me")
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(server.URL)
	if err := client.FlushQueue(t.Context(), local, 10, DefaultQueueBatchBytes); err != nil {
		t.Fatal(err)
	}

	if _, err := service.SetFavorite(t.Context(), rec.ID, true); err != nil {
		t.Fatal(err)
	}
	items, err := local.ListPendingQueue(t.Context(), 10)
	if err != nil || len(items) != 1 || items[0].Action != storage.QueueUpdate || items[0].RecordID != rec.ID {
		t.Fatalf("favorite toggle must enqueue update: %+v, err=%v", items, err)
	}
	if err := client.FlushQueue(t.Context(), local, 10, DefaultQueueBatchBytes); err != nil {
		t.Fatal(err)
	}
	remoteRec, err := remote.GetRecord(t.Context(), rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !remoteRec.IsFavorite || remoteRec.FavoriteAt == nil {
		t.Fatalf("remote favorite not synced: is_favorite=%v favorite_at=%v", remoteRec.IsFavorite, remoteRec.FavoriteAt)
	}

	if _, err := service.SetFavorite(t.Context(), rec.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := client.FlushQueue(t.Context(), local, 10, DefaultQueueBatchBytes); err != nil {
		t.Fatal(err)
	}
	remoteRec, err = remote.GetRecord(t.Context(), rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if remoteRec.IsFavorite || remoteRec.FavoriteAt != nil {
		t.Fatalf("remote unfavorite not synced: is_favorite=%v favorite_at=%v", remoteRec.IsFavorite, remoteRec.FavoriteAt)
	}

	if _, err := service.SetFavorite(t.Context(), "missing", true); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("missing record err = %v, want ErrNotFound", err)
	}
}

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

func TestClientSetFavorite(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/clipboard/records/rec-123/favorite" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			IsFavorite bool `json:"is_favorite"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if !body.IsFavorite {
			t.Errorf("expected is_favorite=true, got %v", body.IsFavorite)
		}
		called = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if err := client.SetFavorite(t.Context(), "rec-123", true); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected handler to be called")
	}
}

func TestClientPromptsMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/clipboard/prompts" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"prompts":[{"id":"p1","title":"Prompt 1","category":"代码","template_content":"Code: {clip}","variables":["clip"]}]}`))
		case r.URL.Path == "/api/v1/clipboard/prompts" && r.Method == http.MethodPost:
			var p domain.PromptTemplate
			_ = json.NewDecoder(r.Body).Decode(&p)
			p.ID = "p2"
			_ = json.NewEncoder(w).Encode(p)
		case r.URL.Path == "/api/v1/clipboard/prompts/p1/use" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"status":"recorded"}`))
		case r.URL.Path == "/api/v1/clipboard/prompts/p1" && r.Method == http.MethodDelete:
			_, _ = w.Write([]byte(`{"status":"deleted"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)

	// List
	list, err := client.ListPrompts(t.Context(), "代码", "")
	if err != nil || len(list) != 1 || list[0].Title != "Prompt 1" {
		t.Fatalf("unexpected list: %+v, err=%v", list, err)
	}

	// Save
	saved, err := client.SavePrompt(t.Context(), &domain.PromptTemplate{Title: "New Prompt", TemplateContent: "Hi {name}"})
	if err != nil || saved.ID != "p2" {
		t.Fatalf("unexpected save: %+v, err=%v", saved, err)
	}

	// Use
	if err := client.UsePrompt(t.Context(), "p1"); err != nil {
		t.Fatalf("unexpected use error: %v", err)
	}

	// Delete
	if err := client.DeletePrompt(t.Context(), "p1"); err != nil {
		t.Fatalf("unexpected delete error: %v", err)
	}
}

func TestActorDeviceIDLoopbackFiltering(t *testing.T) {
	remote := storage.NewMemoryStore()
	server := httptest.NewServer(httpapi.NewServer(remote, ""))
	defer server.Close()

	clientActor := NewClient(server.URL)
	clientActor.DeviceID = "dev-actor"

	// Create a record whose source is dev-origin, but mutated by dev-actor
	rec := domain.ClipboardRecord{
		ID:             "rec-shared-1",
		UserID:         "local",
		ContentType:    domain.ContentText,
		ContentText:    "shared text",
		SourceDeviceID: "dev-origin",
	}

	actorReceived := make(chan domain.SyncEvent, 10)
	otherReceived := make(chan domain.SyncEvent, 10)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go func() {
		_ = clientActor.Subscribe(ctx, "dev-actor", func(e domain.SyncEvent) error {
			actorReceived <- e
			return nil
		})
	}()

	clientOther := NewClient(server.URL)
	go func() {
		_ = clientOther.Subscribe(ctx, "dev-other", func(e domain.SyncEvent) error {
			otherReceived <- e
			return nil
		})
	}()

	// Wait for SSE subscriptions to connect
	time.Sleep(100 * time.Millisecond)

	// Actor pushes record
	if _, err := clientActor.PushRecord(ctx, rec); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-otherReceived:
		if event.DeviceID != "dev-actor" {
			t.Fatalf("expected broadcast event.DeviceID = 'dev-actor', got %q", event.DeviceID)
		}
		if event.RecordID != rec.ID {
			t.Fatalf("expected record id %q, got %q", rec.ID, event.RecordID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event on other device")
	}

	// Actor should NOT receive its own event due to loopback suppression
	select {
	case event := <-actorReceived:
		t.Fatalf("actor received own event: %+v", event)
	case <-time.After(150 * time.Millisecond):
		// Success
	}

	// Actor toggles favorite
	if err := clientActor.SetFavorite(ctx, rec.ID, true); err != nil {
		t.Fatal(err)
	}

	select {
	case event := <-otherReceived:
		if event.DeviceID != "dev-actor" || event.EventType != domain.EventRecordUpdated {
			t.Fatalf("unexpected favorite event: %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for favorite event on other device")
	}

	select {
	case event := <-actorReceived:
		t.Fatalf("actor received own favorite event: %+v", event)
	case <-time.After(150 * time.Millisecond):
		// Success
	}
}
