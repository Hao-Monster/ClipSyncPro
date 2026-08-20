package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clipsyncpro/internal/agent"
	"clipsyncpro/internal/autostart"
	"clipsyncpro/internal/domain"
	"clipsyncpro/internal/nativeui"
	"clipsyncpro/internal/singleinstance"
	"clipsyncpro/internal/storage"
)

func main() {
	lock, alreadyRunning, err := singleinstance.Acquire("Global\\ClipSyncProWindowsClient")
	if err != nil {
		log.Fatalf("single instance lock failed: %v", err)
	}
	defer lock.Release()
	if err := autostart.EnsureEnabled("ClipSyncPro"); err != nil {
		log.Printf("enable startup failed: %v", err)
	}
	if alreadyRunning {
		singleinstance.ActivateExistingWindow("ClipSyncProNativeWindow", "ClipSync Pro")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.OpenMySQLFromEnv(ctx)
	if err != nil {
		log.Fatalf("open local MySQL failed: %v", err)
	}
	defer store.Close()

	host, _ := os.Hostname()
	deviceID := firstEnv("CLIPSYNC_DEVICE_ID")
	if deviceID == "" {
		deviceID = "windows-" + host
	}
	serverURL := firstEnv("CLIPSYNC_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://127.0.0.1:37617"
	}
	secret := firstEnv("CLIPSYNC_SECRET")
	if secret == "" {
		log.Printf("CLIPSYNC_SECRET is not set; sensitive record encryption will fail until configured")
	}

	refreshUI := make(chan struct{}, 1)
	notifyUI := func() {
		select {
		case refreshUI <- struct{}{}:
		default:
		}
	}
	background := agent.New(store, agent.Config{
		ServerURL:    serverURL,
		Secret:       secret,
		UserID:       firstEnv("CLIPSYNC_USER_ID"),
		DeviceID:     deviceID,
		PollInterval: 250 * time.Millisecond,
		OnRecord:     func(_ domain.ClipboardRecord) { notifyUI() },
		OnRemoteEvent: func(_ domain.SyncEvent) {
			notifyUI()
		},
	})
	background.StartBackground(ctx)
	if err := nativeui.Run(ctx, background.Service(), refreshUI); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}
