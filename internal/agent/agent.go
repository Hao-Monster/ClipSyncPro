package agent

import (
	"context"
	"log"
	"net/http"
	"time"

	"clipsyncpro/internal/app"
	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/httpapi"
	"clipsyncpro/internal/storage"
	"clipsyncpro/internal/syncer"
	"clipsyncpro/internal/win32"
)

type Config struct {
	UIAddr        string
	ServerURL     string
	Secret        string
	UserID        string
	DeviceID      string
	PollInterval  time.Duration
	OnRecord      func(domain.ClipboardRecord)
	OnRemoteEvent func(domain.SyncEvent)
}

type Agent struct {
	cfg     Config
	store   storage.Store
	service *app.Service
	sync    *syncer.Client
	reveals revealCache
	events  *httpapi.Hub
}

func New(store storage.Store, cfg Config) *Agent {
	if cfg.UIAddr == "" {
		cfg.UIAddr = "127.0.0.1:37618"
	}
	if cfg.DeviceID == "" {
		cfg.DeviceID = "windows-default"
	}
	if cfg.UserID == "" {
		cfg.UserID = "local"
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 250 * time.Millisecond
	}
	service := app.NewService(store, app.Config{
		Secret:   cfg.Secret,
		UserID:   cfg.UserID,
		DeviceID: cfg.DeviceID,
		Platform: domain.PlatformWindows,
	})
	var syncClient *syncer.Client
	if cfg.ServerURL != "" {
		syncClient = syncer.NewClient(cfg.ServerURL)
	}
	return &Agent{
		cfg:     cfg,
		store:   store,
		service: service,
		sync:    syncClient,
		reveals: newRevealCache(),
		events:  httpapi.NewHub(),
	}
}

func (a *Agent) Run(ctx context.Context) error {
	a.StartBackground(ctx)
	go a.runHotkey(ctx)
	log.Printf("ClipSync Pro debug UI listening on http://%s", a.cfg.UIAddr)
	return httpapi.ListenAndServe(ctx, a.cfg.UIAddr, a)
}

func (a *Agent) RunBridge(ctx context.Context) error {
	a.StartBackground(ctx)
	log.Printf("ClipSync Pro local UI bridge listening on http://%s", a.cfg.UIAddr)
	return httpapi.ListenAndServe(ctx, a.cfg.UIAddr, a)
}

func (a *Agent) StartBackground(ctx context.Context) {
	go a.runClipboardLoop(ctx)
	if a.sync != nil {
		go a.runSyncLoop(ctx)
		go a.runEventLoop(ctx)
	}
}

func (a *Agent) Service() *app.Service {
	return a.service
}

func (a *Agent) runClipboardLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.PollInterval)
	defer ticker.Stop()
	lastSeq := win32.ClipboardSequenceNumber()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			snapshot, err := win32.ReadClipboard()
			if err != nil {
				continue
			}
			if snapshot.Sequence == lastSeq {
				continue
			}
			lastSeq = snapshot.Sequence
			if !snapshot.HasText && !snapshot.HasImage {
				continue
			}
			rec, err := a.captureSnapshot(ctx, snapshot)
			if err != nil {
				log.Printf("clipboard capture failed: %v", err)
				continue
			}
			if a.cfg.OnRecord != nil {
				a.cfg.OnRecord(rec)
			}
			a.broadcastLocalEvent(domain.SyncEvent{
				EventType: domain.EventRecordCreated,
				Record:    &rec,
				Occurred:  time.Now().UTC(),
				DeviceID:  a.cfg.DeviceID,
			})
		}
	}
}

func (a *Agent) captureSnapshot(ctx context.Context, snapshot win32.ClipboardSnapshot) (domain.ClipboardRecord, error) {
	if snapshot.HasImage {
		return a.service.CaptureImage(ctx, snapshot.ImageMIMEType, snapshot.ImageData)
	}
	return a.service.CaptureText(ctx, snapshot.Text)
}

func (a *Agent) runSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.sync.FlushQueue(ctx, a.store, 50); err != nil {
				log.Printf("sync flush failed: %v", err)
			}
		}
	}
}

func (a *Agent) runEventLoop(ctx context.Context) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		err := a.sync.Subscribe(ctx, a.cfg.DeviceID, func(event domain.SyncEvent) error {
			switch event.EventType {
			case domain.EventRecordCreated, domain.EventRecordUpdated:
				if event.Record != nil {
					if err := a.store.UpsertRecord(ctx, *event.Record); err != nil {
						return err
					}
				}
			case domain.EventRecordDeleted:
				if event.RecordID != "" {
					if err := a.store.DeleteRecords(ctx, []string{event.RecordID}); err != nil {
						return err
					}
				}
			}
			if a.cfg.OnRemoteEvent != nil {
				a.cfg.OnRemoteEvent(event)
			}
			a.broadcastLocalEvent(event)
			return nil
		})
		if ctx.Err() != nil {
			return
		}
		log.Printf("sync event stream disconnected: %v", err)
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (a *Agent) broadcastLocalEvent(event domain.SyncEvent) {
	if a.events != nil {
		a.events.Broadcast(event)
	}
}

func (a *Agent) runHotkey(ctx context.Context) {
	err := win32.RegisterAltV(ctx, func() {
		if err := win32.OpenURL("http://" + a.cfg.UIAddr); err != nil {
			log.Printf("open UI failed: %v", err)
		}
	})
	if err != nil && ctx.Err() == nil {
		log.Printf("Alt+V hotkey unavailable: %v", err)
	}
}

var _ http.Handler = (*Agent)(nil)
