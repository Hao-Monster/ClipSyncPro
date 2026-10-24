package storage

import (
	"context"
	"testing"
	"time"

	"clipsyncpro/internal/domain"
)

func TestMemoryStoreListAndDelete(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 6, 1, 15, 30, 0, 0, time.Local)
	rec, err := domain.NewClipboardRecord("local", "dev", domain.PlatformWindows, "hello world", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRecord(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	records, err := store.ListRecords(context.Background(), Filter{Keyword: "world"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("got %d records", len(records))
	}
	if err := store.DeleteRecords(context.Background(), []string{rec.ID}); err != nil {
		t.Fatal(err)
	}
	records, err = store.ListRecords(context.Background(), Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected delete to remove record")
	}
}

func TestMemoryQueueCoalescesLatestActionPerRecord(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	firstID, err := store.Enqueue(t.Context(), SyncQueueItem{RecordID: "rec-1", Action: QueueCreate})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	secondID, err := store.Enqueue(t.Context(), SyncQueueItem{RecordID: "rec-1", Action: QueueDelete})
	if err != nil {
		t.Fatal(err)
	}
	if secondID != firstID {
		t.Fatalf("queue id changed: %d -> %d", firstID, secondID)
	}
	items, err := store.ListPendingQueue(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Action != QueueDelete {
		t.Fatalf("queue was not compacted to the latest action: %+v", items)
	}
}

func TestMemoryQueueBacksOffFailedItems(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	id, err := store.Enqueue(t.Context(), SyncQueueItem{RecordID: "rec-1", Action: QueueCreate})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueueRetry(t.Context(), id, "offline"); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListPendingQueue(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("failed item retried immediately: %+v", items)
	}
	now = now.Add(QueueRetryDelay(1))
	items, err = store.ListPendingQueue(t.Context(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].RetryCount != 1 || items[0].LastError != "offline" {
		t.Fatalf("failed item did not become due: %+v", items)
	}
}

func TestQueueRetryDelayIsCapped(t *testing.T) {
	if got := QueueRetryDelay(1); got != 5*time.Second {
		t.Fatalf("first delay = %v", got)
	}
	if got := QueueRetryDelay(100); got != 15*time.Minute {
		t.Fatalf("capped delay = %v", got)
	}
}

func TestMemoryQueuePrioritizesUntriedItemsOverDueRetries(t *testing.T) {
	store := NewMemoryStore()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	failedID, err := store.Enqueue(t.Context(), SyncQueueItem{RecordID: "failed", Action: QueueCreate})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQueueRetry(t.Context(), failedID, "offline"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := store.Enqueue(t.Context(), SyncQueueItem{RecordID: "untried", Action: QueueCreate}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(QueueRetryDelay(1))
	items, err := store.ListPendingQueue(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].RecordID != "untried" {
		t.Fatalf("queue selected %+v, want the untried item", items)
	}
}

func TestMemoryStoreSummaryListOmitsImageBody(t *testing.T) {
	store := NewMemoryStore()
	rec, err := domain.NewClipboardImageRecord("local", "dev", domain.PlatformWindows, "image/png", []byte("large-image-body"), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertRecord(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	records, err := store.ListRecordSummaries(context.Background(), Filter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("record count = %d", len(records))
	}
	if records[0].ContentText != "" {
		t.Fatalf("image body leaked into summary list: %d bytes", len(records[0].ContentText))
	}
}
