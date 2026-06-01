package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"clipsyncpro/internal/domain"
)

type MySQLStore struct {
	db *sql.DB
}

func OpenMySQLFromEnv(ctx context.Context) (*MySQLStore, error) {
	if raw := os.Getenv("CLIPSYNC_MYSQL_DSN"); raw != "" {
		db, err := sql.Open("mysql", raw)
		if err != nil {
			return nil, err
		}
		if err := db.PingContext(ctx); err != nil {
			_ = db.Close()
			return nil, err
		}
		store := &MySQLStore{db: db}
		return store, store.EnsureSchema(ctx)
	}

	user := firstEnv("mysql_user", "MYSQL_USER", "CLIPSYNC_MYSQL_USER")
	password := firstEnv("mysql_pwd", "MYSQL_PWD", "CLIPSYNC_MYSQL_PASSWORD")
	addr := firstEnv("CLIPSYNC_MYSQL_ADDR", "MYSQL_ADDR")
	if addr == "" {
		addr = "127.0.0.1:3306"
	}
	dbName := firstEnv("CLIPSYNC_MYSQL_DB", "MYSQL_DATABASE")
	if dbName == "" {
		dbName = "clipsync_pro"
	}
	if user == "" {
		return nil, errors.New("mysql_user or CLIPSYNC_MYSQL_DSN is required")
	}
	if !safeDBName(dbName) {
		return nil, fmt.Errorf("unsafe MySQL database name %q", dbName)
	}

	adminDSN := fmt.Sprintf("%s:%s@tcp(%s)/?parseTime=true&charset=utf8mb4,utf8&multiStatements=true", user, password, addr)
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		return nil, err
	}
	if err := admin.PingContext(ctx); err != nil {
		_ = admin.Close()
		return nil, err
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS `"+dbName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		_ = admin.Close()
		return nil, err
	}
	_ = admin.Close()

	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&charset=utf8mb4,utf8&multiStatements=true", user, password, addr, dbName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := &MySQLStore{db: db}
	return store, store.EnsureSchema(ctx)
}

func (s *MySQLStore) Close() error {
	return s.db.Close()
}

func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schemaSQL)
	return err
}

func (s *MySQLStore) UpsertRecord(ctx context.Context, rec domain.ClipboardRecord) error {
	contentCipher, err := jsonOrNull(rec.ContentCipher)
	if err != nil {
		return err
	}
	accountCipher, err := jsonOrNull(rec.AccountCipher)
	if err != nil {
		return err
	}
	passwordCipher, err := jsonOrNull(rec.PasswordCipher)
	if err != nil {
		return err
	}
	remarkCipher, err := jsonOrNull(rec.RemarkCipher)
	if err != nil {
		return err
	}
	searchIndex, err := json.Marshal(rec.SearchIndex)
	if err != nil {
		return err
	}
	var serverCreatedAt any
	if rec.ServerCreatedAt != nil {
		serverCreatedAt = *rec.ServerCreatedAt
	}
	var deletedAt any
	if rec.DeletedAt != nil {
		deletedAt = *rec.DeletedAt
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO clipboard_record (
    id, user_id, content_type, content_text, content_cipher, content_hash,
    is_sensitive, sensitive_type, account_cipher, password_cipher, remark_cipher,
    search_index, source_device_id, source_platform, client_created_at,
    server_created_at, updated_at, deleted_at, sync_status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    user_id=VALUES(user_id),
    content_type=VALUES(content_type),
    content_text=VALUES(content_text),
    content_cipher=VALUES(content_cipher),
    content_hash=VALUES(content_hash),
    is_sensitive=VALUES(is_sensitive),
    sensitive_type=VALUES(sensitive_type),
    account_cipher=VALUES(account_cipher),
    password_cipher=VALUES(password_cipher),
    remark_cipher=VALUES(remark_cipher),
    search_index=VALUES(search_index),
    source_device_id=VALUES(source_device_id),
    source_platform=VALUES(source_platform),
    client_created_at=VALUES(client_created_at),
    server_created_at=VALUES(server_created_at),
    updated_at=VALUES(updated_at),
    deleted_at=VALUES(deleted_at),
    sync_status=VALUES(sync_status)`,
		rec.ID, rec.UserID, rec.ContentType, nullIfEmpty(rec.ContentText), contentCipher, rec.ContentHash,
		rec.IsSensitive, nullIfEmpty(string(rec.SensitiveType)), accountCipher, passwordCipher, remarkCipher,
		string(searchIndex), rec.SourceDeviceID, rec.SourcePlatform, rec.ClientCreatedAt,
		serverCreatedAt, rec.UpdatedAt, deletedAt, rec.SyncStatus)
	return err
}

func (s *MySQLStore) GetRecord(ctx context.Context, id string) (domain.ClipboardRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, content_type, content_text, content_cipher, content_hash,
       is_sensitive, sensitive_type, account_cipher, password_cipher, remark_cipher,
       search_index, source_device_id, source_platform, client_created_at,
       server_created_at, updated_at, deleted_at, sync_status
FROM clipboard_record WHERE id = ? LIMIT 1`, id)
	if err != nil {
		return domain.ClipboardRecord{}, err
	}
	defer rows.Close()
	if rows.Next() {
		return scanRecord(rows)
	}
	return domain.ClipboardRecord{}, ErrNotFound
}

func (s *MySQLStore) ListRecords(ctx context.Context, filter Filter) ([]domain.ClipboardRecord, error) {
	clauses := []string{"1=1"}
	args := make([]any, 0)
	if filter.UserID != "" {
		clauses = append(clauses, "user_id = ?")
		args = append(args, filter.UserID)
	}
	if !filter.IncludeDeleted {
		clauses = append(clauses, "deleted_at IS NULL")
	}
	if filter.SensitiveOnly {
		clauses = append(clauses, "is_sensitive = 1")
	}
	if filter.DateStart != nil {
		clauses = append(clauses, "client_created_at >= ?")
		args = append(args, *filter.DateStart)
	}
	if filter.DateEnd != nil {
		clauses = append(clauses, "client_created_at <= ?")
		args = append(args, *filter.DateEnd)
	}
	if filter.Keyword != "" {
		clauses = append(clauses, "(LOWER(content_text) LIKE ? OR is_sensitive = 1)")
		args = append(args, "%"+strings.ToLower(filter.Keyword)+"%")
	}
	query := `
SELECT id, user_id, content_type, content_text, content_cipher, content_hash,
       is_sensitive, sensitive_type, account_cipher, password_cipher, remark_cipher,
       search_index, source_device_id, source_platform, client_created_at,
       server_created_at, updated_at, deleted_at, sync_status
FROM clipboard_record
WHERE ` + strings.Join(clauses, " AND ") + `
ORDER BY client_created_at DESC`
	if filter.Limit > 0 && filter.Keyword == "" {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]domain.ClipboardRecord, 0)
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		if RecordMatchesFilter(rec, filter) {
			records = append(records, rec)
		}
		if filter.Limit > 0 && len(records) >= filter.Limit {
			break
		}
	}
	return records, rows.Err()
}

func (s *MySQLStore) DeleteRecords(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM clipboard_record WHERE id IN ("+placeholders+")", args...)
	return err
}

func (s *MySQLStore) Enqueue(ctx context.Context, item SyncQueueItem) (int64, error) {
	now := time.Now()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.Status == "" {
		item.Status = QueuePending
	}
	result, err := s.db.ExecContext(ctx, `
INSERT INTO sync_queue (record_id, action, payload, retry_count, status, last_error, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		item.RecordID, item.Action, item.Payload, item.RetryCount, item.Status, nullIfEmpty(item.LastError), item.CreatedAt, item.UpdatedAt)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func (s *MySQLStore) ListPendingQueue(ctx context.Context, limit int) ([]SyncQueueItem, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, record_id, action, payload, retry_count, status, last_error, created_at, updated_at
FROM sync_queue
WHERE status IN ('pending', 'failed')
ORDER BY created_at ASC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SyncQueueItem, 0)
	for rows.Next() {
		var item SyncQueueItem
		var lastError sql.NullString
		if err := rows.Scan(&item.ID, &item.RecordID, &item.Action, &item.Payload, &item.RetryCount, &item.Status, &lastError, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.LastError = lastError.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *MySQLStore) MarkQueueDone(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sync_queue SET status='done', updated_at=? WHERE id=?", time.Now(), id)
	return err
}

func (s *MySQLStore) MarkQueueRetry(ctx context.Context, id int64, errText string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE sync_queue
SET status='failed', retry_count=retry_count+1, last_error=?, updated_at=?
WHERE id=?`, errText, time.Now(), id)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(scanner rowScanner) (domain.ClipboardRecord, error) {
	var rec domain.ClipboardRecord
	var contentText, contentCipher, sensitiveType, accountCipher, passwordCipher, remarkCipher, searchIndex sql.NullString
	var serverCreatedAt, deletedAt sql.NullTime
	if err := scanner.Scan(
		&rec.ID, &rec.UserID, &rec.ContentType, &contentText, &contentCipher, &rec.ContentHash,
		&rec.IsSensitive, &sensitiveType, &accountCipher, &passwordCipher, &remarkCipher,
		&searchIndex, &rec.SourceDeviceID, &rec.SourcePlatform, &rec.ClientCreatedAt,
		&serverCreatedAt, &rec.UpdatedAt, &deletedAt, &rec.SyncStatus,
	); err != nil {
		return domain.ClipboardRecord{}, err
	}
	rec.ContentText = contentText.String
	rec.SensitiveType = domain.SensitiveType(sensitiveType.String)
	rec.ServerCreatedAt = timePtr(serverCreatedAt)
	rec.DeletedAt = timePtr(deletedAt)
	rec.ContentCipher = parseCipher(contentCipher)
	rec.AccountCipher = parseCipher(accountCipher)
	rec.PasswordCipher = parseCipher(passwordCipher)
	rec.RemarkCipher = parseCipher(remarkCipher)
	if searchIndex.Valid && searchIndex.String != "" {
		_ = json.Unmarshal([]byte(searchIndex.String), &rec.SearchIndex)
	}
	return rec, nil
}

func jsonOrNull(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func parseCipher(raw sql.NullString) *domain.CipherText {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var box domain.CipherText
	if err := json.Unmarshal([]byte(raw.String), &box); err != nil {
		return nil
	}
	return &box
}

func timePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

var dbNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func safeDBName(name string) bool {
	return dbNamePattern.MatchString(name)
}
