package storage

import (
	"context"
	"errors"
	"strings"
	"time"

	"clipsyncpro/internal/domain"
)

var ErrNotFound = errors.New("record not found")

type Filter struct {
	UserID         string
	Keyword        string
	KeywordHashes  []string
	DateStart      *time.Time
	DateEnd        *time.Time
	SensitiveOnly  bool
	IncludeDeleted bool
	Limit          int
}

type QueueAction string

const (
	QueueCreate QueueAction = "create"
	QueueUpdate QueueAction = "update"
	QueueDelete QueueAction = "delete"
)

type QueueStatus string

const (
	QueuePending QueueStatus = "pending"
	QueueSyncing QueueStatus = "syncing"
	QueueDone    QueueStatus = "done"
	QueueFailed  QueueStatus = "failed"
)

type SyncQueueItem struct {
	ID         int64       `json:"id"`
	RecordID   string      `json:"record_id"`
	Action     QueueAction `json:"action"`
	Payload    string      `json:"payload"`
	RetryCount int         `json:"retry_count"`
	Status     QueueStatus `json:"status"`
	LastError  string      `json:"last_error,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type Store interface {
	UpsertRecord(ctx context.Context, rec domain.ClipboardRecord) error
	GetRecord(ctx context.Context, id string) (domain.ClipboardRecord, error)
	ListRecords(ctx context.Context, filter Filter) ([]domain.ClipboardRecord, error)
	DeleteRecords(ctx context.Context, ids []string) error
	Enqueue(ctx context.Context, item SyncQueueItem) (int64, error)
	ListPendingQueue(ctx context.Context, limit int) ([]SyncQueueItem, error)
	MarkQueueDone(ctx context.Context, id int64) error
	MarkQueueRetry(ctx context.Context, id int64, errText string) error
}

func RecordMatchesFilter(rec domain.ClipboardRecord, filter Filter) bool {
	if filter.UserID != "" && rec.UserID != filter.UserID {
		return false
	}
	if !filter.IncludeDeleted && rec.DeletedAt != nil {
		return false
	}
	if filter.SensitiveOnly && !rec.IsSensitive {
		return false
	}
	if filter.DateStart != nil && rec.ClientCreatedAt.Before(*filter.DateStart) {
		return false
	}
	if filter.DateEnd != nil && rec.ClientCreatedAt.After(*filter.DateEnd) {
		return false
	}
	keyword := strings.ToLower(filter.Keyword)
	if keyword != "" {
		if !rec.IsSensitive && rec.ContentType != domain.ContentImage && strings.Contains(strings.ToLower(rec.ContentText), keyword) {
			return true
		}
		if rec.IsSensitive && domain.ContainsAll(rec.SearchIndex, filter.KeywordHashes) {
			return true
		}
		return false
	}
	return true
}
