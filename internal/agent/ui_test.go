package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
		if event.Record != nil {
			t.Fatal("local event must not embed the complete record")
		}
		if event.RecordID != rec.ID {
			t.Fatalf("event record id = %q", event.RecordID)
		}
		if event.DeviceID != "dev-1" {
			t.Fatalf("event device id = %q", event.DeviceID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update event")
	}
}

func TestAgentListsAtMostTenMostRecentRecords(t *testing.T) {
	store := storage.NewMemoryStore()
	a := New(store, Config{DeviceID: "dev-1"})
	ctx := context.Background()
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		rec, err := domain.NewClipboardRecord("local", "dev-1", domain.PlatformWindows, fmt.Sprintf("item-%02d", i), base.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.UpsertRecord(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}

	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/records", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status code = %d, body = %s", rr.Code, rr.Body.String())
	}
	var response struct {
		Records []struct {
			Summary string `json:"summary"`
		} `json:"records"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if len(response.Records) != 10 {
		t.Fatalf("record count = %d, want 10", len(response.Records))
	}
	if response.Records[0].Summary != "item-11" || response.Records[9].Summary != "item-02" {
		t.Fatalf("unexpected order: first=%q last=%q", response.Records[0].Summary, response.Records[9].Summary)
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

func TestAgentFavoriteFlowAndBatchDeleteProtection(t *testing.T) {
	store := storage.NewMemoryStore()
	a := New(store, Config{Secret: "secret", DeviceID: "dev-1"})
	ctx := context.Background()

	normalRec, err := a.service.CaptureText(ctx, "normal clipboard item")
	if err != nil {
		t.Fatal(err)
	}
	favRec, err := a.service.CaptureText(ctx, "starred snippet")
	if err != nil {
		t.Fatal(err)
	}

	// 1. 设置为收藏
	favBody := []byte(`{"is_favorite": true}`)
	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/records/"+favRec.ID+"/favorite", bytes.NewReader(favBody)))
	if rr.Code != http.StatusOK {
		t.Fatalf("favorite record failed: %d, body: %s", rr.Code, rr.Body.String())
	}

	// 2. 按 favorite=true 筛选
	listRr := httptest.NewRecorder()
	a.ServeHTTP(listRr, httptest.NewRequest(http.MethodGet, "/api/records?favorite=true", nil))
	if listRr.Code != http.StatusOK {
		t.Fatalf("list favorite records failed: %d", listRr.Code)
	}
	var favListResp struct {
		Records []struct {
			ID         string `json:"id"`
			IsFavorite bool   `json:"is_favorite"`
		} `json:"records"`
	}
	if err := json.NewDecoder(listRr.Body).Decode(&favListResp); err != nil {
		t.Fatal(err)
	}
	if len(favListResp.Records) != 1 || favListResp.Records[0].ID != favRec.ID || !favListResp.Records[0].IsFavorite {
		t.Fatalf("unexpected favorite filter result: %+v", favListResp)
	}

	// 3. 批量删除并跳过收藏记录
	batchBody := []byte(`{"ids":["` + normalRec.ID + `","` + favRec.ID + `"],"skip_favorite":true}`)
	delRr := httptest.NewRecorder()
	a.ServeHTTP(delRr, httptest.NewRequest(http.MethodPost, "/api/records/batch-delete", bytes.NewReader(batchBody)))
	if delRr.Code != http.StatusOK {
		t.Fatalf("batch delete failed: %d, %s", delRr.Code, delRr.Body.String())
	}
	var batchResp struct {
		Count   int `json:"count"`
		Skipped int `json:"skipped"`
	}
	if err := json.NewDecoder(delRr.Body).Decode(&batchResp); err != nil {
		t.Fatal(err)
	}
	if batchResp.Count != 1 || batchResp.Skipped != 1 {
		t.Fatalf("unexpected batch delete counts: %+v", batchResp)
	}

	// normalRec 应该被删除，favRec 依然存在
	if _, err := store.GetRecord(ctx, normalRec.ID); err != storage.ErrNotFound {
		t.Fatalf("normal record should be deleted, got err %v", err)
	}
	if _, err := store.GetRecord(ctx, favRec.ID); err != nil {
		t.Fatalf("favorite record must be preserved: %v", err)
	}
}

func TestAgentPromptTemplatesFlow(t *testing.T) {
	store := storage.NewMemoryStore()
	a := New(store, Config{Secret: "secret", DeviceID: "dev-1"})

	// 1. 获取默认提示词
	listRr := httptest.NewRecorder()
	a.ServeHTTP(listRr, httptest.NewRequest(http.MethodGet, "/api/prompts", nil))
	if listRr.Code != http.StatusOK {
		t.Fatalf("list prompts failed: %d", listRr.Code)
	}
	var listResp struct {
		Prompts []domain.PromptTemplate `json:"prompts"`
	}
	if err := json.NewDecoder(listRr.Body).Decode(&listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Prompts) == 0 {
		t.Fatal("expected default prompts to be populated")
	}

	// 2. 创建新提示词
	newPrompt := domain.NewPromptTemplate("SQL 优化专家", "数据库", "请优化以下 SQL 查询：\n\n{sql}", "/sql")
	body, _ := json.Marshal(newPrompt)
	saveRr := httptest.NewRecorder()
	a.ServeHTTP(saveRr, httptest.NewRequest(http.MethodPost, "/api/prompts", bytes.NewReader(body)))
	if saveRr.Code != http.StatusOK {
		t.Fatalf("save prompt failed: %d, body: %s", saveRr.Code, saveRr.Body.String())
	}

	// 3. 调用计数自增
	useRr := httptest.NewRecorder()
	a.ServeHTTP(useRr, httptest.NewRequest(http.MethodPost, "/api/prompts/"+newPrompt.ID+"/use", nil))
	if useRr.Code != http.StatusOK {
		t.Fatalf("use prompt failed: %d", useRr.Code)
	}
	p, err := store.GetPrompt(context.Background(), newPrompt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.UsageCount != 1 {
		t.Fatalf("expected usage count = 1, got %d", p.UsageCount)
	}

	// 4. 删除提示词
	delRr := httptest.NewRecorder()
	a.ServeHTTP(delRr, httptest.NewRequest(http.MethodDelete, "/api/prompts/"+newPrompt.ID, nil))
	if delRr.Code != http.StatusOK {
		t.Fatalf("delete prompt failed: %d", delRr.Code)
	}
	if _, err := store.GetPrompt(context.Background(), newPrompt.ID); err != storage.ErrNotFound {
		t.Fatalf("expected prompt to be deleted, got err %v", err)
	}
}

func TestAgentFavoriteEnqueuesSyncUpdate(t *testing.T) {
	store := storage.NewMemoryStore()
	a := New(store, Config{DeviceID: "dev-1"})
	rec, err := a.Service().CaptureText(context.Background(), "favorite me")
	if err != nil {
		t.Fatal(err)
	}
	queued, err := store.ListPendingQueue(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range queued {
		if err := store.MarkQueueDone(context.Background(), item.ID); err != nil {
			t.Fatal(err)
		}
	}

	rr := httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/records/"+rec.ID+"/favorite", bytes.NewReader([]byte(`{"is_favorite":true}`))))
	if rr.Code != http.StatusOK {
		t.Fatalf("favorite status = %d, body = %s", rr.Code, rr.Body.String())
	}
	stored, err := store.GetRecord(context.Background(), rec.ID)
	if err != nil || !stored.IsFavorite {
		t.Fatalf("record not favorited: %+v, err=%v", stored, err)
	}
	items, err := store.ListPendingQueue(context.Background(), 10)
	if err != nil || len(items) != 1 || items[0].RecordID != rec.ID || items[0].Action != storage.QueueUpdate {
		t.Fatalf("expected one queued update for favorite toggle, got %+v, err=%v", items, err)
	}

	rr = httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/records/missing/favorite", bytes.NewReader([]byte(`{"is_favorite":true}`))))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing record favorite status = %d, want 404", rr.Code)
	}

	rr = httptest.NewRecorder()
	a.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/prompts/missing/use", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing prompt use status = %d, want 404", rr.Code)
	}
}
