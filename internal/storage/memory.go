package storage

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"clipsyncpro/internal/domain"
)

type MemoryStore struct {
	mu      sync.RWMutex
	records map[string]domain.ClipboardRecord
	prompts map[string]*domain.PromptTemplate
	queue   []SyncQueueItem
	nextID  int64
	now     func() time.Time
}

func NewMemoryStore() *MemoryStore {
	store := &MemoryStore{
		records: make(map[string]domain.ClipboardRecord),
		prompts: make(map[string]*domain.PromptTemplate),
		nextID:  1,
		now:     time.Now,
	}
	for _, p := range domain.DefaultPrompts() {
		store.prompts[p.ID] = p
	}
	return store
}

func (s *MemoryStore) UpsertRecord(_ context.Context, rec domain.ClipboardRecord) error {
	var err error
	rec, err = domain.ClipboardRecordForStorage(rec)
	if err != nil {
		return err
	}
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
	if filter.Offset > 0 {
		if filter.Offset >= len(records) {
			records = nil
		} else {
			records = records[filter.Offset:]
		}
	}
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
			records[i].ImageMIMEType = ""
			records[i].ImageData = nil
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
	now := s.now()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	item.NextAttemptAt = now
	if item.Status == "" {
		item.Status = QueuePending
	}
	for i := range s.queue {
		if s.queue[i].RecordID == item.RecordID {
			s.queue[i].Action = item.Action
			s.queue[i].RetryCount = 0
			s.queue[i].Status = QueuePending
			s.queue[i].LastError = ""
			s.queue[i].NextAttemptAt = now
			s.queue[i].UpdatedAt = now
			return s.queue[i].ID, nil
		}
	}
	item.ID = s.nextID
	s.nextID++
	s.queue = append(s.queue, item)
	return item.ID, nil
}

func (s *MemoryStore) ListPendingQueue(_ context.Context, limit int) ([]SyncQueueItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := s.now()
	items := make([]SyncQueueItem, 0, len(s.queue))
	for _, item := range s.queue {
		if (item.Status == QueuePending || item.Status == QueueFailed) && !item.NextAttemptAt.After(now) {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RetryCount != items[j].RetryCount {
			return items[i].RetryCount < items[j].RetryCount
		}
		if !items[i].NextAttemptAt.Equal(items[j].NextAttemptAt) {
			return items[i].NextAttemptAt.Before(items[j].NextAttemptAt)
		}
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
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
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
			s.queue[i].UpdatedAt = s.now()
			s.queue[i].NextAttemptAt = s.queue[i].UpdatedAt.Add(QueueRetryDelay(s.queue[i].RetryCount))
			return nil
		}
	}
	return nil
}

func (s *MemoryStore) SetFavorite(_ context.Context, id string, isFavorite bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[id]
	if !ok {
		return ErrNotFound
	}
	rec.IsFavorite = isFavorite
	if isFavorite {
		now := s.now()
		rec.FavoriteAt = &now
	} else {
		rec.FavoriteAt = nil
	}
	rec.UpdatedAt = s.now()
	s.records[id] = rec
	return nil
}

func (s *MemoryStore) ListPrompts(_ context.Context, category, keyword string) ([]*domain.PromptTemplate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	list := make([]*domain.PromptTemplate, 0, len(s.prompts))
	cat := strings.TrimSpace(category)
	kw := strings.ToLower(strings.TrimSpace(keyword))
	for _, p := range s.prompts {
		if cat != "" && cat != "全部" && p.Category != cat {
			continue
		}
		if kw != "" {
			if !strings.Contains(strings.ToLower(p.Title), kw) &&
				!strings.Contains(strings.ToLower(p.TemplateContent), kw) &&
				!strings.Contains(strings.ToLower(p.Shortcut), kw) {
				continue
			}
		}
		copied := *p
		list = append(list, &copied)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].IsFavorite != list[j].IsFavorite {
			return list[i].IsFavorite
		}
		if list[i].UsageCount != list[j].UsageCount {
			return list[i].UsageCount > list[j].UsageCount
		}
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
	return list, nil
}

func (s *MemoryStore) GetPrompt(_ context.Context, id string) (*domain.PromptTemplate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.prompts[id]
	if !ok {
		return nil, ErrNotFound
	}
	copied := *p
	return &copied, nil
}

func (s *MemoryStore) SavePrompt(_ context.Context, prompt *domain.PromptTemplate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prompt.ID == "" {
		newP := domain.NewPromptTemplate(prompt.Title, prompt.Category, prompt.TemplateContent, prompt.Shortcut)
		newP.IsFavorite = prompt.IsFavorite
		*prompt = *newP
	}
	prompt.UpdatedAt = s.now()
	if prompt.CreatedAt.IsZero() {
		prompt.CreatedAt = prompt.UpdatedAt
	}
	prompt.Variables = domain.ExtractVariables(prompt.TemplateContent)
	s.prompts[prompt.ID] = prompt
	return nil
}

func (s *MemoryStore) DeletePrompt(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.prompts, id)
	return nil
}

func (s *MemoryStore) IncrementPromptUsage(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.prompts[id]
	if !ok {
		return ErrNotFound
	}
	p.UsageCount++
	p.UpdatedAt = s.now()
	return nil
}
