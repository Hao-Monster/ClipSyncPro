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

var ErrLegacyMigrationRequired = errors.New("legacy ClipSync Pro data migration is required")

func OpenMySQLFromEnv(ctx context.Context) (*MySQLStore, error) {
	store, err := openMySQLFromEnv(ctx, true)
	if err != nil {
		return nil, err
	}
	if err := store.ensureApplicationReady(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

// OpenMySQLForMigrationFromEnv prepares additive schema but deliberately
// bypasses the application startup gate so the guarded migration can run.
func OpenMySQLForMigrationFromEnv(ctx context.Context) (*MySQLStore, error) {
	return openMySQLFromEnv(ctx, true)
}

// OpenMySQLReadOnlyFromEnv connects to an existing database without creating
// or altering schema. It is used by migration planning commands.
func OpenMySQLReadOnlyFromEnv(ctx context.Context) (*MySQLStore, error) {
	return openMySQLFromEnv(ctx, false)
}

func openMySQLFromEnv(ctx context.Context, prepareSchema bool) (*MySQLStore, error) {
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
		if prepareSchema {
			return store, store.EnsureSchema(ctx)
		}
		return store, nil
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

	if prepareSchema {
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
	}

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
	if prepareSchema {
		return store, store.EnsureSchema(ctx)
	}
	return store, nil
}

func (s *MySQLStore) Close() error {
	return s.db.Close()
}

func (s *MySQLStore) DatabaseName(ctx context.Context) (string, error) {
	var name string
	if err := s.db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil {
		return "", err
	}
	return name, nil
}

func (s *MySQLStore) EnsureSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	columns := []struct {
		name       string
		definition string
	}{
		{name: "image_mime", definition: "VARCHAR(64) NULL AFTER content_text"},
		{name: "image_blob", definition: "LONGBLOB NULL AFTER image_mime"},
		{name: "is_favorite", definition: "TINYINT(1) NOT NULL DEFAULT 0 AFTER deleted_at"},
		{name: "favorite_at", definition: "DATETIME(3) NULL AFTER is_favorite"},
	}
	for _, column := range columns {
		if err := s.ensureColumn(ctx, "clipboard_record", column.name, column.definition); err != nil {
			return err
		}
	}
	if err := s.ensureIndex(ctx, "clipboard_record", "idx_favorite", "(user_id, is_favorite, favorite_at)"); err != nil {
		return err
	}
	if err := s.ensureIndex(ctx, "sync_queue_v2", "idx_sync_queue_schedule", "(status, retry_count, next_attempt_at, created_at)"); err != nil {
		return err
	}
	// 初始化内置提示词种子
	var promptCount int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM prompt_template").Scan(&promptCount); err == nil && promptCount == 0 {
		for _, p := range domain.DefaultPrompts() {
			_ = s.SavePrompt(ctx, p)
		}
	}
	return nil
}

func (s *MySQLStore) ensureApplicationReady(ctx context.Context) error {
	var legacyImages int64
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM clipboard_record
WHERE content_type = 'image' AND content_text IS NOT NULL AND image_blob IS NULL`).Scan(&legacyImages); err != nil {
		return err
	}
	var legacyQueue int64
	exists, err := s.tableExists(ctx, "sync_queue")
	if err != nil {
		return err
	}
	if exists {
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sync_queue WHERE status IN ('pending', 'failed')").Scan(&legacyQueue); err != nil {
			return err
		}
	}
	if legacyImages > 0 || legacyQueue > 0 {
		return fmt.Errorf("%w: %d image rows and %d queue rows; run clipsync-migrate before starting the application", ErrLegacyMigrationRequired, legacyImages, legacyQueue)
	}
	return nil
}

func (s *MySQLStore) ensureColumn(ctx context.Context, table, column, definition string) error {
	exists, err := s.columnExists(ctx, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if !safeIdentifier(table) || !safeIdentifier(column) {
		return errors.New("unsafe schema identifier")
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE `"+table+"` ADD COLUMN `"+column+"` "+definition)
	return err
}

func (s *MySQLStore) columnExists(ctx context.Context, table, column string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, table, column).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *MySQLStore) tableExists(ctx context.Context, table string) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?`, table).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *MySQLStore) ensureIndex(ctx context.Context, table, index, definition string) error {
	if !safeIdentifier(table) || !safeIdentifier(index) {
		return errors.New("unsafe schema identifier")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.STATISTICS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?`, table, index).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, "ALTER TABLE `"+table+"` ADD INDEX `"+index+"` "+definition)
	return err
}

func (s *MySQLStore) UpsertRecord(ctx context.Context, rec domain.ClipboardRecord) error {
	var err error
	rec, err = domain.ClipboardRecordForStorage(rec)
	if err != nil {
		return err
	}
	contentText := rec.ContentText
	var imageMIME, imageData any
	if rec.ContentType == domain.ContentImage {
		imageMIME = rec.ImageMIMEType
		imageData = rec.ImageData
	}
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
	var favoriteAt any
	if rec.FavoriteAt != nil {
		favoriteAt = *rec.FavoriteAt
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO clipboard_record (
    id, user_id, content_type, content_text, image_mime, image_blob, content_cipher, content_hash,
    is_sensitive, sensitive_type, account_cipher, password_cipher, remark_cipher,
    search_index, source_device_id, source_platform, client_created_at,
    server_created_at, updated_at, deleted_at, is_favorite, favorite_at, sync_status
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    user_id=VALUES(user_id),
    content_type=VALUES(content_type),
    content_text=VALUES(content_text),
    image_mime=VALUES(image_mime),
    image_blob=VALUES(image_blob),
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
    is_favorite=VALUES(is_favorite),
    favorite_at=VALUES(favorite_at),
    sync_status=VALUES(sync_status)`,
		rec.ID, rec.UserID, rec.ContentType, nullIfEmpty(contentText), imageMIME, imageData, contentCipher, rec.ContentHash,
		rec.IsSensitive, nullIfEmpty(string(rec.SensitiveType)), accountCipher, passwordCipher, remarkCipher,
		string(searchIndex), rec.SourceDeviceID, rec.SourcePlatform, rec.ClientCreatedAt,
		serverCreatedAt, rec.UpdatedAt, deletedAt, rec.IsFavorite, favoriteAt, rec.SyncStatus)
	return err
}

func (s *MySQLStore) GetRecord(ctx context.Context, id string) (domain.ClipboardRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, user_id, content_type, content_text, image_mime, image_blob, content_cipher, content_hash,
       is_sensitive, sensitive_type, account_cipher, password_cipher, remark_cipher,
       search_index, source_device_id, source_platform, client_created_at,
       server_created_at, updated_at, deleted_at, is_favorite, favorite_at, sync_status
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
	return s.listRecords(ctx, filter, false)
}

func (s *MySQLStore) ListRecordSummaries(ctx context.Context, filter Filter) ([]domain.ClipboardRecord, error) {
	return s.listRecords(ctx, filter, true)
}

func (s *MySQLStore) listRecords(ctx context.Context, filter Filter, summariesOnly bool) ([]domain.ClipboardRecord, error) {
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
	if filter.FavoriteOnly {
		clauses = append(clauses, "is_favorite = 1")
	}
	if filter.ExcludeFavorite {
		clauses = append(clauses, "is_favorite = 0")
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
		clauses = append(clauses, "((content_type <> 'image' AND LOWER(content_text) LIKE ?) OR is_sensitive = 1)")
		args = append(args, "%"+strings.ToLower(filter.Keyword)+"%")
	}
	contentColumn := "content_text"
	imageMIMEColumn := "image_mime"
	imageDataColumn := "image_blob"
	if summariesOnly {
		contentColumn = "CASE WHEN content_type = 'image' THEN NULL ELSE content_text END"
		imageMIMEColumn = "NULL"
		imageDataColumn = "NULL"
	}
	query := `
SELECT id, user_id, content_type, ` + contentColumn + ` AS content_text,
       ` + imageMIMEColumn + ` AS image_mime, ` + imageDataColumn + ` AS image_blob,
       content_cipher, content_hash,
       is_sensitive, sensitive_type, account_cipher, password_cipher, remark_cipher,
       search_index, source_device_id, source_platform, client_created_at,
       server_created_at, updated_at, deleted_at, is_favorite, favorite_at, sync_status
FROM clipboard_record
WHERE ` + strings.Join(clauses, " AND ") + `
ORDER BY client_created_at DESC`
	if filter.Limit > 0 && filter.Keyword == "" {
		query += " LIMIT ? OFFSET ?"
		args = append(args, filter.Limit, filter.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := make([]domain.ClipboardRecord, 0)
	matches := 0
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		if RecordMatchesFilter(rec, filter) {
			if filter.Keyword != "" && matches < filter.Offset {
				matches++
				continue
			}
			matches++
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
	result, err := s.db.ExecContext(ctx, `
INSERT INTO sync_queue_v2 (
    record_id, action, retry_count, status, last_error, next_attempt_at, created_at, updated_at
)
VALUES (?, ?, 0, 'pending', NULL, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    id=LAST_INSERT_ID(id),
    action=VALUES(action),
    retry_count=0,
    status='pending',
    last_error=NULL,
    next_attempt_at=VALUES(next_attempt_at),
    updated_at=VALUES(updated_at)`,
		item.RecordID, item.Action, now, item.CreatedAt, item.UpdatedAt)
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
SELECT id, record_id, action, retry_count, status, last_error, next_attempt_at, created_at, updated_at
FROM sync_queue_v2
WHERE status IN ('pending', 'failed')
  AND next_attempt_at <= ?
ORDER BY retry_count ASC, next_attempt_at ASC, created_at ASC
LIMIT ?`, time.Now(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SyncQueueItem, 0)
	for rows.Next() {
		var item SyncQueueItem
		var lastError sql.NullString
		if err := rows.Scan(&item.ID, &item.RecordID, &item.Action, &item.RetryCount, &item.Status, &lastError, &item.NextAttemptAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.LastError = lastError.String
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *MySQLStore) MarkQueueDone(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sync_queue_v2 WHERE id=?", id)
	return err
}

func (s *MySQLStore) MarkQueueRetry(ctx context.Context, id int64, errText string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var retryCount int
	if err := tx.QueryRowContext(ctx, "SELECT retry_count FROM sync_queue_v2 WHERE id=? FOR UPDATE", id).Scan(&retryCount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	now := time.Now()
	retryCount++
	_, err = tx.ExecContext(ctx, `
UPDATE sync_queue_v2
SET status='failed', retry_count=?, last_error=?, next_attempt_at=?, updated_at=?
WHERE id=?`, retryCount, truncateRunes(errText, 4000), now.Add(QueueRetryDelay(retryCount)), now, id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRecord(scanner rowScanner) (domain.ClipboardRecord, error) {
	var rec domain.ClipboardRecord
	var contentText, imageMIME, contentCipher, sensitiveType, accountCipher, passwordCipher, remarkCipher, searchIndex sql.NullString
	var imageData []byte
	var serverCreatedAt, deletedAt, favoriteAt sql.NullTime
	if err := scanner.Scan(
		&rec.ID, &rec.UserID, &rec.ContentType, &contentText, &imageMIME, &imageData, &contentCipher, &rec.ContentHash,
		&rec.IsSensitive, &sensitiveType, &accountCipher, &passwordCipher, &remarkCipher,
		&searchIndex, &rec.SourceDeviceID, &rec.SourcePlatform, &rec.ClientCreatedAt,
		&serverCreatedAt, &rec.UpdatedAt, &deletedAt, &rec.IsFavorite, &favoriteAt, &rec.SyncStatus,
	); err != nil {
		return domain.ClipboardRecord{}, err
	}
	rec.ContentText = contentText.String
	rec.ImageMIMEType = imageMIME.String
	rec.ImageData = imageData
	rec.SensitiveType = domain.SensitiveType(sensitiveType.String)
	rec.ServerCreatedAt = timePtr(serverCreatedAt)
	rec.DeletedAt = timePtr(deletedAt)
	rec.FavoriteAt = timePtr(favoriteAt)
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

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
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

func safeIdentifier(name string) bool {
	return dbNamePattern.MatchString(name)
}

func (s *MySQLStore) SetFavorite(ctx context.Context, id string, isFavorite bool) error {
	now := time.Now()
	var favoriteAt any
	if isFavorite {
		favoriteAt = now
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE clipboard_record
SET is_favorite=?, favorite_at=?, updated_at=?
WHERE id=?`, isFavorite, favoriteAt, now, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// MySQL 默认返回实际变更行数：同一毫秒内重复写入相同值会得到 0，需要二次确认记录是否存在。
		var exists int
		err := s.db.QueryRowContext(ctx, "SELECT 1 FROM clipboard_record WHERE id = ? LIMIT 1", id).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *MySQLStore) ListPrompts(ctx context.Context, category, keyword string) ([]*domain.PromptTemplate, error) {
	clauses := []string{"1=1"}
	args := make([]any, 0)
	cat := strings.TrimSpace(category)
	if cat != "" && cat != "全部" {
		clauses = append(clauses, "category = ?")
		args = append(args, cat)
	}
	kw := strings.TrimSpace(keyword)
	if kw != "" {
		clauses = append(clauses, "(LOWER(title) LIKE ? OR LOWER(template_content) LIKE ? OR LOWER(shortcut) LIKE ?)")
		pattern := "%" + strings.ToLower(kw) + "%"
		args = append(args, pattern, pattern, pattern)
	}
	query := `
SELECT id, title, category, template_content, variables_json, shortcut, usage_count, is_favorite, created_at, updated_at
FROM prompt_template
WHERE ` + strings.Join(clauses, " AND ") + `
ORDER BY is_favorite DESC, usage_count DESC, updated_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]*domain.PromptTemplate, 0)
	for rows.Next() {
		var p domain.PromptTemplate
		var variablesJSON, shortcut sql.NullString
		if err := rows.Scan(&p.ID, &p.Title, &p.Category, &p.TemplateContent, &variablesJSON, &shortcut, &p.UsageCount, &p.IsFavorite, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Shortcut = shortcut.String
		if variablesJSON.Valid && variablesJSON.String != "" {
			_ = json.Unmarshal([]byte(variablesJSON.String), &p.Variables)
		}
		if p.Variables == nil {
			p.Variables = domain.ExtractVariables(p.TemplateContent)
		}
		list = append(list, &p)
	}
	return list, rows.Err()
}

func (s *MySQLStore) GetPrompt(ctx context.Context, id string) (*domain.PromptTemplate, error) {
	var p domain.PromptTemplate
	var variablesJSON, shortcut sql.NullString
	err := s.db.QueryRowContext(ctx, `
SELECT id, title, category, template_content, variables_json, shortcut, usage_count, is_favorite, created_at, updated_at
FROM prompt_template WHERE id = ? LIMIT 1`, id).Scan(
		&p.ID, &p.Title, &p.Category, &p.TemplateContent, &variablesJSON, &shortcut, &p.UsageCount, &p.IsFavorite, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	p.Shortcut = shortcut.String
	if variablesJSON.Valid && variablesJSON.String != "" {
		_ = json.Unmarshal([]byte(variablesJSON.String), &p.Variables)
	}
	if p.Variables == nil {
		p.Variables = domain.ExtractVariables(p.TemplateContent)
	}
	return &p, nil
}

func (s *MySQLStore) SavePrompt(ctx context.Context, prompt *domain.PromptTemplate) error {
	if prompt.ID == "" {
		newP := domain.NewPromptTemplate(prompt.Title, prompt.Category, prompt.TemplateContent, prompt.Shortcut)
		newP.IsFavorite = prompt.IsFavorite
		*prompt = *newP
	} else {
		// 编辑已有模板时保留服务端维护的使用次数与创建时间，避免客户端提交旧值覆盖。
		existing, err := s.GetPrompt(ctx, prompt.ID)
		switch {
		case err == nil:
			prompt.UsageCount = existing.UsageCount
			prompt.CreatedAt = existing.CreatedAt
		case !errors.Is(err, ErrNotFound):
			return err
		}
	}
	prompt.Variables = domain.ExtractVariables(prompt.TemplateContent)
	varJSON, _ := json.Marshal(prompt.Variables)
	now := time.Now()
	prompt.UpdatedAt = now
	if prompt.CreatedAt.IsZero() {
		prompt.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO prompt_template (
    id, title, category, template_content, variables_json, shortcut, usage_count, is_favorite, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    title=VALUES(title),
    category=VALUES(category),
    template_content=VALUES(template_content),
    variables_json=VALUES(variables_json),
    shortcut=VALUES(shortcut),
    is_favorite=VALUES(is_favorite),
    updated_at=VALUES(updated_at)`,
		prompt.ID, prompt.Title, prompt.Category, prompt.TemplateContent, string(varJSON), nullIfEmpty(prompt.Shortcut),
		prompt.UsageCount, prompt.IsFavorite, prompt.CreatedAt, prompt.UpdatedAt)
	return err
}

func (s *MySQLStore) DeletePrompt(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM prompt_template WHERE id = ?", id)
	return err
}

func (s *MySQLStore) IncrementPromptUsage(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE prompt_template SET usage_count = usage_count + 1, updated_at = ? WHERE id = ?", time.Now(), id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}
