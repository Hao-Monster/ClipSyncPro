package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"clipsyncpro/internal/agent"
	"clipsyncpro/internal/singleinstance"
	"clipsyncpro/internal/storage"
)

func main() {
	lock, alreadyRunning, err := singleinstance.Acquire("Global\\ClipSyncProWindowsAgent")
	if err != nil {
		log.Fatalf("single instance lock failed: %v", err)
	}
	defer lock.Release()
	if alreadyRunning {
		log.Printf("ClipSync Pro agent is already running")
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
	uiAddr := firstEnv("CLIPSYNC_UI_ADDR")
	if uiAddr == "" {
		uiAddr = "127.0.0.1:37618"
	}

	background := agent.New(store, agent.Config{
		UIAddr:       uiAddr,
		ServerURL:    serverURL,
		Secret:       secret,
		UserID:       firstEnv("CLIPSYNC_USER_ID"),
		DeviceID:     deviceID,
		PollInterval: 250 * time.Millisecond,
	})
	if err := background.RunBridge(ctx); err != nil && ctx.Err() == nil {
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
