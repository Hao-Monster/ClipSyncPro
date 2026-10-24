package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"clipsyncpro/internal/agent"
	"clipsyncpro/internal/singleinstance"
	"clipsyncpro/internal/storage"
)

func main() {
	closeLog := configureLog()
	defer closeLog()

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

	if err := runWithStorageRetry(ctx, uiAddr, agent.Config{
		UIAddr:       uiAddr,
		ServerURL:    serverURL,
		Secret:       secret,
		UserID:       firstEnv("CLIPSYNC_USER_ID"),
		DeviceID:     deviceID,
		PollInterval: 250 * time.Millisecond,
	}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

func runWithStorageRetry(ctx context.Context, uiAddr string, cfg agent.Config) error {
	var degraded *http.Server
	for {
		store, err := storage.OpenMySQLFromEnv(ctx)
		if err != nil {
			log.Printf("open local MySQL failed: %v; retrying in 5s", err)
			if degraded == nil {
				server := &http.Server{
					Addr:              uiAddr,
					Handler:           degradedHandler{},
					ReadHeaderTimeout: 5 * time.Second,
				}
				degraded = server
				go func() {
					if serveErr := server.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
						log.Printf("degraded local UI failed: %v", serveErr)
					}
				}()
			}
			select {
			case <-ctx.Done():
				if degraded != nil {
					_ = degraded.Shutdown(context.Background())
				}
				return nil
			case <-time.After(5 * time.Second):
				continue
			}
		}

		if degraded != nil {
			_ = degraded.Shutdown(context.Background())
			degraded = nil
		}
		background := agent.New(store, cfg)
		runErr := background.RunBridge(ctx)
		_ = store.Close()
		if ctx.Err() != nil {
			return nil
		}
		log.Printf("local agent stopped: %v; retrying in 2s", runErr)
		time.Sleep(2 * time.Second)
	}
}

type degradedHandler struct{}

func (degradedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/api/status" {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":         "degraded",
			"ui_api_version": "1",
			"device_id":      "windows-local",
			"user_id":        "local",
			"ui_addr":        r.Host,
			"storage_status": "unavailable",
		})
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error": "storage unavailable",
		"code":  "storage_unavailable",
	})
}

func configureLog() func() {
	logDir := os.Getenv("CLIPSYNC_LOG_DIR")
	if logDir == "" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			return func() {}
		}
		logDir = filepath.Join(cacheDir, "ClipSync Pro")
	}
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return func() {}
	}
	file, err := os.OpenFile(filepath.Join(logDir, "agent.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return func() {}
	}
	log.SetOutput(io.MultiWriter(os.Stderr, file))
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	return func() { _ = file.Close() }
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}
