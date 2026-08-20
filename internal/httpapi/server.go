package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	case strings.HasPrefix(path, "/api/v1/clipboard/records/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(path, "/api/v1/clipboard/records/")
		s.handleDelete(w, r, id)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	}
}

func (s *Server) handleUpsert(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var rec domain.ClipboardRecord
	if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
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
		Record:    &rec,
		Occurred:  now,
		DeviceID:  rec.SourceDeviceID,
	})
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	filter, ok := s.filterFromQuery(w, r)
	if !ok {
		return
	}
	records, err := s.store.ListRecords(r.Context(), filter)
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
		Limit:         500,
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
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if err := s.store.DeleteRecords(r.Context(), body.IDs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete failed"})
		return
	}
	now := s.now().UTC()
	for _, id := range body.IDs {
		s.hub.Broadcast(domain.SyncEvent{
			EventType: domain.EventRecordDeleted,
			RecordID:  id,
			Occurred:  now,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "count": len(body.IDs)})
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
