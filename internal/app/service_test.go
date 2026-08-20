package app

import (
	"bytes"
	"context"
	"testing"

	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/storage"
)

func TestCaptureImagePersistsAndQueuesRecord(t *testing.T) {
	store := storage.NewMemoryStore()
	service := NewService(store, Config{UserID: "local", DeviceID: "dev", Platform: domain.PlatformWindows})
	data := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	rec, err := service.CaptureImage(context.Background(), "image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ContentType != domain.ContentImage {
		t.Fatalf("content type = %q", rec.ContentType)
	}
	stored, err := store.GetRecord(context.Background(), rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := domain.DecodeClipboardImage(stored.ContentText)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("stored image mismatch: %v %x", err, got)
	}
	queue, err := store.ListPendingQueue(context.Background(), 10)
	if err != nil || len(queue) != 1 || queue[0].Action != storage.QueueCreate {
		t.Fatalf("unexpected queue: %+v, err=%v", queue, err)
	}
}

func TestCaptureMarkRevealSearchDelete(t *testing.T) {
	store := storage.NewMemoryStore()
	service := NewService(store, Config{
		Secret:   "secret",
		UserID:   "local",
		DeviceID: "dev",
		Platform: domain.PlatformWindows,
	})
	ctx := context.Background()
	rec, err := service.CaptureText(ctx, "p@ssw0rd")
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	rec, err = service.MarkPassword(ctx, rec.ID, "alice@example.com", "mail")
	if err != nil {
		t.Fatalf("mark: %v", err)
	}
	if rec.ContentText != "" || !rec.IsSensitive {
		t.Fatal("expected sensitive record with plaintext cleared")
	}
	revealed, err := service.Reveal(ctx, rec.ID)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if revealed.Account != "alice@example.com" || revealed.Password != "p@ssw0rd" {
		t.Fatalf("unexpected reveal: %+v", revealed)
	}
	records, err := service.List(ctx, storage.Filter{Keyword: "alice", SensitiveOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected sensitive search hit, got %d", len(records))
	}
	if err := service.Delete(ctx, []string{rec.ID}); err != nil {
		t.Fatal(err)
	}
	records, err = service.List(ctx, storage.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatal("expected record deleted")
	}
}

func TestMarkAccountRevealReturnsAccountAndPassword(t *testing.T) {
	store := storage.NewMemoryStore()
	service := NewService(store, Config{
		Secret:   "secret",
		UserID:   "local",
		DeviceID: "dev",
		Platform: domain.PlatformWindows,
	})
	ctx := context.Background()
	rec, err := service.CaptureText(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if _, err := service.MarkAccount(ctx, rec.ID, "p@ssw0rd", "mail"); err != nil {
		t.Fatalf("mark account: %v", err)
	}
	revealed, err := service.Reveal(ctx, rec.ID)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if revealed.Account != "alice@example.com" {
		t.Fatalf("account not revealed: %+v", revealed)
	}
	if revealed.Password != "p@ssw0rd" {
		t.Fatalf("password not revealed: %+v", revealed)
	}
	if revealed.Content != "alice@example.com" {
		t.Fatalf("original content should remain decryptable: %+v", revealed)
	}
}

func TestUpdateRemarkReencryptsAndUpdatesSearchIndex(t *testing.T) {
	store := storage.NewMemoryStore()
	service := NewService(store, Config{
		Secret:   "secret",
		UserID:   "local",
		DeviceID: "dev",
		Platform: domain.PlatformWindows,
	})
	ctx := context.Background()
	rec, err := service.CaptureText(ctx, "p@ssw0rd")
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if _, err := service.MarkPassword(ctx, rec.ID, "alice@example.com", "old remark"); err != nil {
		t.Fatalf("mark password: %v", err)
	}
	if _, err := service.UpdateRemark(ctx, rec.ID, "new private note"); err != nil {
		t.Fatalf("update remark: %v", err)
	}
	revealed, err := service.Reveal(ctx, rec.ID)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if revealed.Remark != "new private note" {
		t.Fatalf("remark not updated: %+v", revealed)
	}
	records, err := service.List(ctx, storage.Filter{Keyword: "private", SensitiveOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected updated remark to be searchable, got %d", len(records))
	}
}
