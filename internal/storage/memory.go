package storage

import (
	"context"
	"sort"
	"sync"
	"time"

	"clipsyncpro/internal/domain"
)

type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]domain.ClipboardRecord
	queue   []SyncQueueItem
	nextID  int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		records: make(map[string]domain.ClipboardRecord),
		nextID:  1,
	}
}

func (s *MemoryStore) UpsertRecord(_ context.Context, rec domain.ClipboardRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.ID] = rec
	return nil
}

func (s *MemoryStore) GetRecord(_ context.Context, id string) (domain.ClipboardRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.records[id]
	if !ok {
		return domain.ClipboardRecord{}, ErrNotFound
	}
	return rec, nil
}

func (s *MemoryStore) ListRecords(_ context.Context, filter Filter) ([]domain.ClipboardRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]domain.ClipboardRecord, 0, len(s.records))
	for _, rec := range s.records {
		if RecordMatchesFilter(rec, filter) {
			records = append(records, rec)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].ClientCreatedAt.After(records[j].ClientCreatedAt)
	})
	if filter.Limit > 0 && len(records) > filter.Limit {
		records = records[:filter.Limit]
	}
	return records, nil
}

func (s *MemoryStore) ListRecordSummaries(ctx context.Context, filter Filter) ([]domain.ClipboardRecord, error) {
	records, err := s.ListRecords(ctx, filter)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if records[i].ContentType == domain.ContentImage {
			records[i].ContentText = ""
		}
	}
	return records, nil
}

func (s *MemoryStore) DeleteRecords(_ context.Context, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.records, id)
	}
	return nil
}

func (s *MemoryStore) Enqueue(_ context.Context, item SyncQueueItem) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.Status == "" {
		item.Status = QueuePending
	}
	item.ID = s.nextID
	s.nextID++
	s.queue = append(s.queue, item)
	return item.ID, nil
}

func (s *MemoryStore) ListPendingQueue(_ context.Context, limit int) ([]SyncQueueItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]SyncQueueItem, 0, len(s.queue))
	for _, item := range s.queue {
		if item.Status == QueuePending || item.Status == QueueFailed {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *MemoryStore) MarkQueueDone(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.queue {
		if s.queue[i].ID == id {
			s.queue[i].Status = QueueDone
			s.queue[i].UpdatedAt = time.Now()
			return nil
		}
	}
	return nil
}

func (s *MemoryStore) MarkQueueRetry(_ context.Context, id int64, errText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.queue {
		if s.queue[i].ID == id {
			s.queue[i].Status = QueueFailed
			s.queue[i].RetryCount++
			s.queue[i].LastError = errText
			s.queue[i].UpdatedAt = time.Now()
			return nil
		}
	}
	return nil
}
