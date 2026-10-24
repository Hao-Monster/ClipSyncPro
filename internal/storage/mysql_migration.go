package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"clipsyncpro/internal/domain"
)

type LegacyImageMigrationReport struct {
	TotalRows     int64 `json:"total_rows"`
	Found         int64 `json:"legacy_rows"`
	Migrated      int64 `json:"migrated_rows"`
	ImageBytes    int64 `json:"legacy_image_bytes"`
	AlreadyBinary int64 `json:"binary_rows"`
	BinaryBytes   int64 `json:"binary_bytes"`
	DryRun        bool  `json:"dry_run"`
}

type LegacyQueueMigrationReport struct {
	PendingRows     int64 `json:"legacy_pending_rows"`
	UniqueRecords   int64 `json:"legacy_unique_records"`
	PayloadBytes    int64 `json:"legacy_payload_bytes"`
	ActiveV2Rows    int64 `json:"active_v2_rows"`
	MigratedRecords int64 `json:"migrated_records"`
	DryRun          bool  `json:"dry_run"`
}

type legacyImageCandidate struct {
	id          string
	contentText string
	contentHash string
	mimeType    string
	data        []byte
}

// MigrateLegacyImages converts legacy Base64 data URLs to BLOBs in bounded,
// resumable batches. A failed batch is rolled back; previously completed
// batches remain valid and are skipped on the next run.
func (s *MySQLStore) MigrateLegacyImages(ctx context.Context, batchSize int, dryRun bool) (LegacyImageMigrationReport, error) {
	if batchSize <= 0 || batchSize > 100 {
		batchSize = 10
	}
	report := LegacyImageMigrationReport{DryRun: dryRun}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM clipboard_record WHERE content_type = 'image'").Scan(&report.TotalRows); err != nil {
		return report, err
	}
	hasImageBlob, err := s.columnExists(ctx, "clipboard_record", "image_blob")
	if err != nil {
		return report, err
	}
	legacyCondition := ""
	if hasImageBlob {
		legacyCondition = " AND image_blob IS NULL"
		var binaryBytes sql.NullInt64
		if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*), SUM(OCTET_LENGTH(image_blob))
FROM clipboard_record
WHERE content_type = 'image' AND image_blob IS NOT NULL`).Scan(&report.AlreadyBinary, &binaryBytes); err != nil {
			return report, err
		}
		report.BinaryBytes = binaryBytes.Int64
	}
	lastID := ""
	for {
		rows, err := s.db.QueryContext(ctx, `
SELECT id, content_text, content_hash
FROM clipboard_record
WHERE content_type = 'image'
  AND content_text IS NOT NULL
  AND id > ?
  `+legacyCondition+`
ORDER BY id
LIMIT ?`, lastID, batchSize)
		if err != nil {
			return report, err
		}
		candidates := make([]legacyImageCandidate, 0, batchSize)
		batchBytes := 0
		for rows.Next() {
			var candidate legacyImageCandidate
			if err := rows.Scan(&candidate.id, &candidate.contentText, &candidate.contentHash); err != nil {
				rows.Close()
				return report, err
			}
			if err := prepareLegacyImageCandidate(&candidate); err != nil {
				rows.Close()
				return report, fmt.Errorf("legacy image %s: %w", candidate.id, err)
			}
			if len(candidates) > 0 && batchBytes+len(candidate.data) > domain.MaxClipboardImageBytes {
				break
			}
			candidates = append(candidates, candidate)
			batchBytes += len(candidate.data)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return report, err
		}
		rows.Close()
		if len(candidates) == 0 {
			return report, nil
		}
		for _, candidate := range candidates {
			report.Found++
			report.ImageBytes += int64(len(candidate.data))
		}
		lastID = candidates[len(candidates)-1].id
		if dryRun {
			continue
		}
		if !hasImageBlob {
			return report, errors.New("image BLOB columns are missing; prepare schema before apply")
		}
		migrated, err := s.applyLegacyImageBatch(ctx, candidates)
		if err != nil {
			return report, err
		}
		report.Migrated += migrated
	}
}

func prepareLegacyImageCandidate(candidate *legacyImageCandidate) error {
	mimeType, data, err := domain.DecodeClipboardImage(candidate.contentText)
	if err != nil {
		return err
	}
	if candidate.contentHash != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(candidate.contentHash, hex.EncodeToString(sum[:])) {
			return fmt.Errorf("content hash mismatch")
		}
	}
	candidate.mimeType = mimeType
	candidate.data = data
	return nil
}

func (s *MySQLStore) applyLegacyImageBatch(ctx context.Context, candidates []legacyImageCandidate) (migrated int64, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	for _, candidate := range candidates {
		result, execErr := tx.ExecContext(ctx, `
UPDATE clipboard_record
SET image_mime = ?, image_blob = ?, content_text = NULL
WHERE id = ? AND image_blob IS NULL`, candidate.mimeType, candidate.data, candidate.id)
		if execErr != nil {
			err = execErr
			return 0, err
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			err = rowsErr
			return 0, err
		}
		migrated += rows
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return migrated, nil
}

// MigrateLegacyQueue compacts the last pending action for each record into the
// payload-free queue. Apply mode intentionally refuses a non-empty v2 queue so
// deployment order mistakes cannot overwrite newer local actions.
func (s *MySQLStore) MigrateLegacyQueue(ctx context.Context, dryRun bool) (LegacyQueueMigrationReport, error) {
	report := LegacyQueueMigrationReport{DryRun: dryRun}
	hasV2, err := s.tableExists(ctx, "sync_queue_v2")
	if err != nil {
		return report, err
	}
	if hasV2 {
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sync_queue_v2").Scan(&report.ActiveV2Rows); err != nil {
			return report, err
		}
	}
	exists, err := s.tableExists(ctx, "sync_queue")
	if err != nil {
		return report, err
	}
	if !exists {
		return report, nil
	}
	var payloadBytes sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*), COUNT(DISTINCT record_id), SUM(OCTET_LENGTH(payload))
FROM sync_queue
WHERE status IN ('pending', 'failed')`).Scan(&report.PendingRows, &report.UniqueRecords, &payloadBytes); err != nil {
		return report, err
	}
	report.PayloadBytes = payloadBytes.Int64
	if dryRun || report.PendingRows == 0 {
		return report, nil
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	var existing int64
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sync_queue_v2 FOR UPDATE").Scan(&existing); err != nil {
		return report, err
	}
	if existing != 0 {
		return report, errors.New("sync_queue_v2 is not empty; migrate before starting the upgraded application")
	}
	now := time.Now()
	result, err := tx.ExecContext(ctx, `
INSERT INTO sync_queue_v2 (
    record_id, action, retry_count, status, last_error, next_attempt_at, created_at, updated_at
)
SELECT q.record_id, q.action, 0, 'pending', NULL, ?, q.created_at, ?
FROM sync_queue q
INNER JOIN (
    SELECT record_id, MAX(id) AS latest_id
    FROM sync_queue
    WHERE status IN ('pending', 'failed')
    GROUP BY record_id
) latest ON latest.latest_id = q.id`, now, now)
	if err != nil {
		return report, err
	}
	report.MigratedRecords, err = result.RowsAffected()
	if err != nil {
		return report, err
	}
	if report.MigratedRecords != report.UniqueRecords {
		return report, fmt.Errorf("queue migration count mismatch: inserted %d, expected %d", report.MigratedRecords, report.UniqueRecords)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE sync_queue
SET status = 'migrated', payload = '', updated_at = ?
WHERE status IN ('pending', 'failed')`, now); err != nil {
		return report, err
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	return report, nil
}
