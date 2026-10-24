package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/security"
	"clipsyncpro/internal/storage"
)

type Server struct {
	store  storage.Store
	secret string
	hub    *Hub
	now    func() time.Time
}

const maxRecordRequestBytes = 90 << 20

func NewServer(store storage.Store, secret string) *Server {
	return &Server{
		store:  store,
		secret: secret,
		hub:    NewHub(),
		now:    time.Now,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		writeCORS(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeCORS(w)
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/healthz":
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case path == "/ws/v1/sync" || path == "/api/v1/sync/events":
		s.handleEvents(w, r)
	case path == "/api/v1/clipboard/records" && r.Method == http.MethodPost:
		s.handleUpsert(w, r)
	case path == "/api/v1/clipboard/records" && r.Method == http.MethodGet:
		s.handleList(w, r)
	case path == "/api/v1/clipboard/records/search" && r.Method == http.MethodGet:
		s.handleList(w, r)
	case path == "/api/v1/clipboard/records/batch-delete" && r.Method == http.MethodPost:
		s.handleBatchDelete(w, r)
	case strings.HasSuffix(path, "/favorite") && strings.HasPrefix(path, "/api/v1/clipboard/records/") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/clipboard/records/"), "/favorite")
		s.handleSetFavorite(w, r, id)
	case path == "/api/v1/clipboard/prompts" && r.Method == http.MethodGet:
		s.handleListPrompts(w, r)
	case path == "/api/v1/clipboard/prompts" && r.Method == http.MethodPost:
		s.handleSavePrompt(w, r)
	case strings.HasPrefix(path, "/api/v1/clipboard/prompts/") && strings.HasSuffix(path, "/use") && r.Method == http.MethodPost:
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/v1/clipboard/prompts/"), "/use")
		s.handleUsePrompt(w, r, id)
	case strings.HasPrefix(path, "/api/v1/clipboard/prompts/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(path, "/api/v1/clipboard/prompts/")
		s.handleDeletePrompt(w, r, id)
	case strings.HasPrefix(path, "/api/v1/clipboard/records/") && r.Method == http.MethodGet:
		id := strings.TrimPrefix(path, "/api/v1/clipboard/records/")
		s.handleGet(w, r, id)
	case strings.HasPrefix(path, "/api/v1/clipboard/records/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(path, "/api/v1/clipboard/records/")
		s.handleDelete(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (s *Server) handleUpsert(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxRecordRequestBytes)
	var rec domain.ClipboardRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "record is too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if rec.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	_, err := s.store.GetRecord(r.Context(), rec.ID)
	eventType := domain.EventRecordUpdated
	if errors.Is(err, storage.ErrNotFound) {
		eventType = domain.EventRecordCreated
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store read failed"})
		return
	}
	now := s.now().UTC()
	if rec.UserID == "" {
		rec.UserID = "local"
	}
	if rec.ClientCreatedAt.IsZero() {
		rec.ClientCreatedAt = now
	}
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	rec.ServerCreatedAt = &now
	rec.SyncStatus = domain.SyncSynced
	if err := s.store.UpsertRecord(r.Context(), rec); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store write failed"})
		return
	}
	s.hub.Broadcast(domain.SyncEvent{
		EventType: eventType,
		RecordID:  rec.ID,
		Occurred:  now,
		DeviceID:  rec.SourceDeviceID,
	})
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	record, err := s.store.GetRecord(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store read failed"})
		return
	}
	wireRecord, err := domain.ClipboardRecordForWire(record)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "record image is invalid"})
		return
	}
	writeJSON(w, http.StatusOK, wireRecord)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	filter, ok := s.filterFromQuery(w, r)
	if !ok {
		return
	}
	records, err := s.store.ListRecordSummaries(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store list failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": records})
}

func (s *Server) filterFromQuery(w http.ResponseWriter, r *http.Request) (storage.Filter, bool) {
	q := r.URL.Query()
	filter := storage.Filter{
		UserID:        q.Get("user_id"),
		Keyword:       strings.TrimSpace(q.Get("keyword")),
		SensitiveOnly: q.Get("sensitive") == "true" || q.Get("sensitive") == "1",
		FavoriteOnly:  q.Get("favorite") == "true" || q.Get("favorite") == "1",
		Limit:         10,
		Offset:        0,
	}
	if raw := q.Get("offset"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			filter.Offset = n
		}
	}
	if filter.UserID == "" {
		filter.UserID = "local"
	}
	if filter.Keyword != "" {
		filter.KeywordHashes = security.BuildQueryIndex(s.secret, filter.Keyword)
	}
	if rawDate := q.Get("date"); rawDate != "" {
		start, end, err := domain.ParseFilterDate(rawDate)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return storage.Filter{}, false
		}
		filter.DateStart = &start
		filter.DateEnd = &end
	}
	return filter, true
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if err := s.store.DeleteRecords(r.Context(), []string{id}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete failed"})
		return
	}
	now := s.now().UTC()
	s.hub.Broadcast(domain.SyncEvent{
		EventType: domain.EventRecordDeleted,
		RecordID:  id,
		Occurred:  now,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleBatchDelete(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var body struct {
		IDs          []string `json:"ids"`
		SkipFavorite bool     `json:"skip_favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	ids := body.IDs
	skipped := 0
	if body.SkipFavorite {
		var toDelete []string
		for _, id := range ids {
			rec, err := s.store.GetRecord(r.Context(), id)
			if err == nil && rec.IsFavorite {
				skipped++
				continue
			}
			toDelete = append(toDelete, id)
		}
		ids = toDelete
	}
	if len(ids) > 0 {
		if err := s.store.DeleteRecords(r.Context(), ids); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete failed"})
			return
		}
		now := s.now().UTC()
		for _, id := range ids {
			s.hub.Broadcast(domain.SyncEvent{
				EventType: domain.EventRecordDeleted,
				RecordID:  id,
				Occurred:  now,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "count": len(ids), "skipped": skipped})
}

func (s *Server) handleSetFavorite(w http.ResponseWriter, r *http.Request, id string) {
	defer r.Body.Close()
	var body struct {
		IsFavorite bool `json:"is_favorite"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.store.SetFavorite(r.Context(), id, body.IsFavorite); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "favorite update failed"})
		return
	}
	now := s.now().UTC()
	s.hub.Broadcast(domain.SyncEvent{
		EventType: domain.EventRecordUpdated,
		RecordID:  id,
		Occurred:  now,
	})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "is_favorite": body.IsFavorite})
}

func (s *Server) handleListPrompts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	category := q.Get("category")
	keyword := q.Get("keyword")
	prompts, err := s.store.ListPrompts(r.Context(), category, keyword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"prompts": prompts})
}

func (s *Server) handleSavePrompt(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var p domain.PromptTemplate
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(p.Title) == "" || strings.TrimSpace(p.TemplateContent) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title and template_content are required"})
		return
	}
	if err := s.store.SavePrompt(r.Context(), &p); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePrompt(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if err := s.store.DeletePrompt(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleUsePrompt(w http.ResponseWriter, r *http.Request, id string) {
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if err := s.store.IncrementPromptUsage(r.Context(), id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "recorded"})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	writeCORS(w)

	ch, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()
		case event := <-ch:
			data, err := json.Marshal(event)
			if err != nil {
				log.Printf("sync event marshal failed: %v", err)
				continue
			}
			fmt.Fprintf(w, "event: sync\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

type Hub struct {
	mu   sync.RWMutex
	subs map[chan domain.SyncEvent]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[chan domain.SyncEvent]struct{})}
}

func (h *Hub) Subscribe() (<-chan domain.SyncEvent, func()) {
	ch := make(chan domain.SyncEvent, 32)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		delete(h.subs, ch)
		close(ch)
		h.mu.Unlock()
	}
	return ch, cancel
}

func (h *Hub) Broadcast(event domain.SyncEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	writeCORS(w)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeCORS(w http.ResponseWriter) {
	// Native clients do not require CORS. Omitting ACAO prevents arbitrary web
	// origins from reading a privately deployed user's clipboard history.
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func ListenAndServe(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
