package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"clipsyncpro/internal/storage"
)

func main() {
	log.SetFlags(0)
	apply := flag.Bool("apply", false, "apply the migration; the default is a read-only plan")
	confirmDatabase := flag.String("confirm-database", "", "exact database name required with --apply")
	backupConfirmed := flag.Bool("backup-confirmed", false, "confirm that a restorable backup exists; required with --apply")
	batchSize := flag.Int("batch-size", 10, "legacy image rows per transaction (1-100, capped at 64 MiB)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	planStore, err := storage.OpenMySQLReadOnlyFromEnv(ctx)
	if err != nil {
		log.Fatalf("open MySQL for migration plan: %v", err)
	}
	databaseName, err := planStore.DatabaseName(ctx)
	if err != nil {
		_ = planStore.Close()
		log.Fatalf("read database name: %v", err)
	}
	if *apply && (*confirmDatabase != databaseName || !*backupConfirmed) {
		_ = planStore.Close()
		log.Fatalf("refusing apply: use --confirm-database=%s and --backup-confirmed after verifying a restorable backup", databaseName)
	}

	store := planStore
	if *apply {
		_ = planStore.Close()
		store, err = storage.OpenMySQLForMigrationFromEnv(ctx)
		if err != nil {
			log.Fatalf("prepare MySQL migration schema: %v", err)
		}
	}
	defer store.Close()

	imageReport, err := store.MigrateLegacyImages(ctx, *batchSize, !*apply)
	if err != nil {
		log.Fatalf("migrate legacy images: %v", err)
	}
	queueReport, err := store.MigrateLegacyQueue(ctx, !*apply)
	if err != nil {
		log.Fatalf("migrate legacy queue: %v", err)
	}
	report := struct {
		Mode     string                             `json:"mode"`
		Database string                             `json:"database"`
		Images   storage.LegacyImageMigrationReport `json:"images"`
		Queue    storage.LegacyQueueMigrationReport `json:"queue"`
	}{
		Mode:     map[bool]string{true: "apply", false: "plan"}[*apply],
		Database: databaseName,
		Images:   imageReport,
		Queue:    queueReport,
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		log.Fatal(fmt.Errorf("write migration report: %w", err))
	}
}
