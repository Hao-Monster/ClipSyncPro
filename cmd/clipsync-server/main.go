package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"clipsyncpro/internal/httpapi"
	"clipsyncpro/internal/storage"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	addr := os.Getenv("CLIPSYNC_SERVER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:37617"
	}
	secret := os.Getenv("CLIPSYNC_SECRET")
	var store storage.Store = storage.NewMemoryStore()
	if os.Getenv("CLIPSYNC_SERVER_STORAGE") == "mysql" {
		mysqlStore, err := storage.OpenMySQLFromEnv(ctx)
		if err != nil {
			log.Fatalf("open server MySQL failed: %v", err)
		}
		defer mysqlStore.Close()
		store = mysqlStore
	}
	server := httpapi.NewServer(store, secret)

	log.Printf("ClipSync Pro server listening on http://%s", addr)
	if err := httpapi.ListenAndServe(ctx, addr, server); err != nil {
		log.Fatal(err)
	}
}
