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

func TestServerFavoriteFlowAndFilter(t *testing.T) {
	store := storage.NewMemoryStore()
	server := NewServer(store, "")
	now := time.Now().UTC()

	rec1, _ := domain.NewClipboardRecord("local", "dev1", domain.PlatformWindows, "normal note", now)
	rec2, _ := domain.NewClipboardRecord("local", "dev1", domain.PlatformWindows, "starred note", now.Add(time.Second))
	_ = store.UpsertRecord(t.Context(), rec1)
	_ = store.UpsertRecord(t.Context(), rec2)

	// Set rec2 as favorite
	body, _ := json.Marshal(map[string]bool{"is_favorite": true})
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/clipboard/records/"+rec2.ID+"/favorite", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rr.Code, rr.Body.String())
	}

	// Verify favorite filter
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/clipboard/records?favorite=true", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var res struct {
		Records []domain.ClipboardRecord `json:"records"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&res)
	if len(res.Records) != 1 || res.Records[0].ID != rec2.ID {
		t.Fatalf("favorite filter returned %d records, want only rec2", len(res.Records))
	}
}

func TestServerBatchDeleteWithSkipFavorite(t *testing.T) {
	store := storage.NewMemoryStore()
	server := NewServer(store, "")
	now := time.Now().UTC()

	rec1, _ := domain.NewClipboardRecord("local", "dev1", domain.PlatformWindows, "item 1", now)
	rec2, _ := domain.NewClipboardRecord("local", "dev1", domain.PlatformWindows, "item 2", now.Add(time.Second))
	_ = store.UpsertRecord(t.Context(), rec1)
	_ = store.UpsertRecord(t.Context(), rec2)
	_ = store.SetFavorite(t.Context(), rec2.ID, true)

	body, _ := json.Marshal(map[string]any{
		"ids":           []string{rec1.ID, rec2.ID},
		"skip_favorite": true,
	})
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/clipboard/records/batch-delete", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var res struct {
		Status  string `json:"status"`
		Count   int    `json:"count"`
		Skipped int    `json:"skipped"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&res)
	if res.Count != 1 || res.Skipped != 1 {
		t.Fatalf("batch delete result: count=%d, skipped=%d, want 1, 1", res.Count, res.Skipped)
	}

	// rec2 should still exist
	if _, err := store.GetRecord(t.Context(), rec2.ID); err != nil {
		t.Fatalf("rec2 was deleted despite skip_favorite: %v", err)
	}
}

func TestServerPromptsFlow(t *testing.T) {
	store := storage.NewMemoryStore()
	server := NewServer(store, "")

	// Create prompt
	newPrompt := domain.PromptTemplate{
		Title:           "Translate to French",
		Category:        "翻译",
		TemplateContent: "Translate this into French: {clip}",
		Shortcut:        "Alt+T",
	}
	body, _ := json.Marshal(newPrompt)
	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/clipboard/prompts", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rr.Code, rr.Body.String())
	}
	var created domain.PromptTemplate
	_ = json.NewDecoder(rr.Body).Decode(&created)
	if created.ID == "" || len(created.Variables) != 1 || created.Variables[0] != "clip" {
		t.Fatalf("unexpected created prompt: %#v", created)
	}

	// List prompts
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/clipboard/prompts?category=翻译", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var listRes struct {
		Prompts []*domain.PromptTemplate `json:"prompts"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&listRes)
	if len(listRes.Prompts) == 0 {
		t.Fatal("expected at least 1 prompt in list")
	}

	// Use prompt
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/clipboard/prompts/"+created.ID+"/use", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	// Delete prompt
	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/v1/clipboard/prompts/"+created.ID, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestServerFavoriteAndPromptUseReturnNotFoundForUnknownIDs(t *testing.T) {
	server := NewServer(storage.NewMemoryStore(), "")

	rr := httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/clipboard/records/missing/favorite", bytes.NewReader([]byte(`{"is_favorite":true}`))))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("favorite status = %d, want 404, body = %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/clipboard/prompts/missing/use", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("prompt use status = %d, want 404, body = %s", rr.Code, rr.Body.String())
	}
}

func TestServerPromptEditPreservesUsageCount(t *testing.T) {
	server := NewServer(storage.NewMemoryStore(), "")

	post := func(p domain.PromptTemplate) domain.PromptTemplate {
		t.Helper()
		body, _ := json.Marshal(p)
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/clipboard/prompts", bytes.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("save status = %d, body = %s", rr.Code, rr.Body.String())
		}
		var saved domain.PromptTemplate
		if err := json.NewDecoder(rr.Body).Decode(&saved); err != nil {
			t.Fatal(err)
		}
		return saved
	}

	created := post(domain.PromptTemplate{Title: "Polish", Category: "写作", TemplateContent: "Polish: {clip}"})
	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		server.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/clipboard/prompts/"+created.ID+"/use", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("use status = %d", rr.Code)
		}
	}

	edited := post(domain.PromptTemplate{ID: created.ID, Title: "Polish v2", Category: "写作", TemplateContent: "Polish {tone:formal}: {clip}", UsageCount: 0})
	if edited.UsageCount != 2 {
		t.Fatalf("usage_count = %d, want 2 (edit must not reset usage)", edited.UsageCount)
	}
	if !edited.CreatedAt.Equal(created.CreatedAt) {
		t.Fatalf("created_at changed on edit: %v -> %v", created.CreatedAt, edited.CreatedAt)
	}
	if len(edited.Variables) != 2 || edited.Variables[0] != "tone" || edited.Variables[1] != "clip" {
		t.Fatalf("variables = %v, want [tone clip]", edited.Variables)
	}
}
