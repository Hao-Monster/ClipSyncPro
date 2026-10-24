package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"clipsyncpro/internal/domain"
)

func TestMySQLLegacyMigrationsAndBinaryImageStorage(t *testing.T) {
	if os.Getenv("CLIPSYNC_MYSQL_INTEGRATION") != "1" {
		t.Skip("set CLIPSYNC_MYSQL_INTEGRATION=1 to run isolated MySQL migration tests")
	}
	user := firstEnv("mysql_user", "MYSQL_USER", "CLIPSYNC_MYSQL_USER")
	password := firstEnv("mysql_pwd", "MYSQL_PWD", "CLIPSYNC_MYSQL_PASSWORD")
	addr := firstEnv("CLIPSYNC_MYSQL_ADDR", "MYSQL_ADDR")
	if addr == "" {
		addr = "127.0.0.1:3306"
	}
	if user == "" {
		t.Fatal("MySQL integration credentials are not configured")
	}
	databaseName := fmt.Sprintf("clipsync_test_%d", time.Now().UnixNano())
	adminDSN := fmt.Sprintf("%s:%s@tcp(%s)/?parseTime=true&charset=utf8mb4,utf8&multiStatements=true", user, password, addr)
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.Exec("CREATE DATABASE `" + databaseName + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !safeDBName(databaseName) || len(databaseName) < len("clipsync_test_1") {
			t.Errorf("refusing to drop unexpected test database %q", databaseName)
			return
		}
		if _, err := admin.Exec("DROP DATABASE `" + databaseName + "`"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	})

	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&charset=utf8mb4,utf8&multiStatements=true", user, password, addr, databaseName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(legacyIntegrationSchema); err != nil {
		t.Fatal(err)
	}
	store := &MySQLStore{db: db}
	ctx := context.Background()
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	imageData := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	rec, err := domain.NewClipboardImageRecord("local", "device", domain.PlatformWindows, "image/png", imageData, now)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := domain.ClipboardRecordForWire(rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO clipboard_record (
    id, user_id, content_type, content_text, content_hash, is_sensitive,
    source_device_id, source_platform, client_created_at, updated_at, sync_status
) VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
		wire.ID, wire.UserID, wire.ContentType, wire.ContentText, wire.ContentHash,
		wire.SourceDeviceID, wire.SourcePlatform, wire.ClientCreatedAt, wire.UpdatedAt, wire.SyncStatus); err != nil {
		t.Fatal(err)
	}
	largePayload := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 1024))
	for _, item := range []struct {
		recordID string
		action   QueueAction
	}{
		{"rec-queue-1", QueueCreate},
		{"rec-queue-1", QueueUpdate},
		{"rec-queue-2", QueueDelete},
	} {
		if _, err := db.Exec(`
INSERT INTO sync_queue (record_id, action, payload, retry_count, status, created_at, updated_at)
VALUES (?, ?, ?, 0, 'pending', ?, ?)`, item.recordID, item.action, largePayload, now, now); err != nil {
			t.Fatal(err)
		}
	}

	imagePlan, err := store.MigrateLegacyImages(ctx, 1, true)
	if err != nil || imagePlan.Found != 1 || imagePlan.Migrated != 0 {
		t.Fatalf("image plan = %+v, err=%v", imagePlan, err)
	}
	queuePlan, err := store.MigrateLegacyQueue(ctx, true)
	if err != nil || queuePlan.PendingRows != 3 || queuePlan.UniqueRecords != 2 || queuePlan.PayloadBytes == 0 {
		t.Fatalf("queue plan = %+v, err=%v", queuePlan, err)
	}
	if err := store.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.ensureApplicationReady(ctx); !errors.Is(err, ErrLegacyMigrationRequired) {
		t.Fatalf("startup gate error = %v, want ErrLegacyMigrationRequired", err)
	}
	imageReport, err := store.MigrateLegacyImages(ctx, 1, false)
	if err != nil || imageReport.Migrated != 1 {
		t.Fatalf("image migration = %+v, err=%v", imageReport, err)
	}
	stored, err := store.GetRecord(ctx, rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ContentText != "" || stored.ImageMIMEType != "image/png" || !bytes.Equal(stored.ImageData, imageData) {
		t.Fatalf("stored image = %+v", stored)
	}
	summaries, err := store.ListRecordSummaries(ctx, Filter{UserID: "local", Limit: 10})
	if err != nil || len(summaries) != 1 {
		t.Fatalf("image summaries = %+v, err=%v", summaries, err)
	}
	if summaries[0].ContentText != "" || summaries[0].ImageMIMEType != "" || len(summaries[0].ImageData) != 0 {
		t.Fatalf("image summary loaded body columns: %+v", summaries[0])
	}
	queueReport, err := store.MigrateLegacyQueue(ctx, false)
	if err != nil || queueReport.MigratedRecords != 2 {
		t.Fatalf("queue migration = %+v, err=%v", queueReport, err)
	}
	items, err := store.ListPendingQueue(ctx, 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("v2 queue = %+v, err=%v", items, err)
	}
	actions := map[string]QueueAction{}
	for _, item := range items {
		actions[item.RecordID] = item.Action
	}
	if actions["rec-queue-1"] != QueueUpdate || actions["rec-queue-2"] != QueueDelete {
		t.Fatalf("migrated actions = %+v", actions)
	}
	var legacyPayloadBytes int64
	if err := db.QueryRow("SELECT COALESCE(SUM(OCTET_LENGTH(payload)), 0) FROM sync_queue").Scan(&legacyPayloadBytes); err != nil {
		t.Fatal(err)
	}
	if legacyPayloadBytes != 0 {
		t.Fatalf("legacy payload bytes = %d, want 0", legacyPayloadBytes)
	}
	if err := store.ensureApplicationReady(ctx); err != nil {
		t.Fatalf("startup gate remained closed after migration: %v", err)
	}
}

const legacyIntegrationSchema = `
CREATE TABLE clipboard_record (
    id VARCHAR(64) PRIMARY KEY,
    user_id VARCHAR(64) NOT NULL,
    content_type VARCHAR(32) NOT NULL,
    content_text LONGTEXT NULL,
    content_cipher LONGTEXT NULL,
    content_hash CHAR(64) NOT NULL,
    is_sensitive TINYINT(1) NOT NULL DEFAULT 0,
    sensitive_type VARCHAR(32) NULL,
    account_cipher LONGTEXT NULL,
    password_cipher LONGTEXT NULL,
    remark_cipher LONGTEXT NULL,
    search_index LONGTEXT NULL,
    source_device_id VARCHAR(64) NOT NULL,
    source_platform VARCHAR(32) NOT NULL,
    client_created_at DATETIME(3) NOT NULL,
    server_created_at DATETIME(3) NULL,
    updated_at DATETIME(3) NOT NULL,
    deleted_at DATETIME(3) NULL,
    sync_status VARCHAR(32) NOT NULL DEFAULT 'pending'
);
CREATE TABLE sync_queue (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    record_id VARCHAR(64) NOT NULL,
    action VARCHAR(32) NOT NULL,
    payload LONGTEXT NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    last_error TEXT NULL,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL
);`
