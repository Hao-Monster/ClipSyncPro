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
