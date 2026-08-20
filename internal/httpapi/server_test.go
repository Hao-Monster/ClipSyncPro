package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/storage"
)

func TestServerDoesNotGrantCrossOriginBrowserAccess(t *testing.T) {
	server := NewServer(storage.NewMemoryStore(), "")
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/clipboard/records", nil)
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unexpected cross-origin access: %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing nosniff header: %q", rr.Header().Get("X-Content-Type-Options"))
	}
}

func TestServerListReturnsOnlyTenRecordSummaries(t *testing.T) {
	store := storage.NewMemoryStore()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		rec, err := domain.NewClipboardRecord("local", "device", domain.PlatformWindows, string(rune('a'+i)), now.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertRecord(t.Context(), rec); err != nil {
			t.Fatal(err)
		}
	}
	image, err := domain.NewClipboardImageRecord("local", "device", domain.PlatformWindows, "image/png", []byte("large-image-body"), now.Add(20*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRecord(t.Context(), image); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	NewServer(store, "").ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/clipboard/records", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Records []domain.ClipboardRecord `json:"records"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Records) != 10 {
		t.Fatalf("record count = %d, want 10", len(response.Records))
	}
	if response.Records[0].ID != image.ID {
		t.Fatalf("first record = %q, want image %q", response.Records[0].ID, image.ID)
	}
	if response.Records[0].ContentText != "" {
		t.Fatalf("image summary included body: %q", response.Records[0].ContentText)
	}
}

func TestServerUpsertBroadcastsLightweightEvent(t *testing.T) {
	store := storage.NewMemoryStore()
	server := NewServer(store, "")
	events, unsubscribe := server.hub.Subscribe()
	defer unsubscribe()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	rec, err := domain.NewClipboardRecord("local", "device", domain.PlatformWindows, "hello", now)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/clipboard/records", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	select {
	case event := <-events:
		if event.RecordID != rec.ID {
			t.Fatalf("record id = %q, want %q", event.RecordID, rec.ID)
		}
		if event.Record != nil {
			t.Fatal("event unexpectedly embedded the full record")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sync event")
	}
}

func TestServerGetReturnsSingleRecord(t *testing.T) {
	store := storage.NewMemoryStore()
	rec, err := domain.NewClipboardRecord("local", "device", domain.PlatformWindows, "hello", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRecord(t.Context(), rec); err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	NewServer(store, "").ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/clipboard/records/"+rec.ID, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got domain.ClipboardRecord
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ID != rec.ID || got.ContentText != rec.ContentText {
		t.Fatalf("record = %#v, want %#v", got, rec)
	}
}
