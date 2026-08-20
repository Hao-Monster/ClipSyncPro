# ClipSync Pro

ClipSync Pro is a private-deployment clipboard history and cross-device sync system for Windows and Android.

This repository currently contains a Go-based MVP foundation that can be built in the current workspace:

- `cmd/clipsync-server`: HTTP sync server with record APIs and Server-Sent Events at the PRD-compatible `/ws/v1/sync` path.
- `cmd/clipsync-windows`: native Windows client with text/image clipboard polling, local MySQL storage, sync queue, per-user autostart, `Alt + V` hotkey hook, and Win32 desktop UI.
- `android`: native Android Kotlin client source with foreground clipboard service, server upload, record list, reveal, copy, and delete flows.
- `internal/domain`: shared record and sync event model.
- `internal/security`: AES-256-GCM encryption and keyed sensitive search indexes.
- `internal/storage`: memory and MySQL storage implementations.

The target product architecture is native Windows desktop + native Android client + Go sync server. The previous local web surface is retained only as an optional debug handler inside `internal/agent`; the default Windows entrypoint is native Win32 UI.

## Quick Start

### Server

```powershell
go run .\cmd\clipsync-server
```

Default server address:

```text
http://127.0.0.1:37617
```

Use MySQL-backed server storage:

```powershell
$env:CLIPSYNC_SERVER_STORAGE = "mysql"
$env:mysql_user = "root"
$env:mysql_pwd = "your-password"
$env:CLIPSYNC_MYSQL_DB = "clipsync_server"
go run .\cmd\clipsync-server
```

### Windows Agent

Configure local MySQL:

```powershell
$env:mysql_user = "root"
$env:mysql_pwd = "your-password"
$env:CLIPSYNC_MYSQL_DB = "clipsync_pro"
$env:CLIPSYNC_SERVER_URL = "http://127.0.0.1:37617"
$env:CLIPSYNC_SECRET = "replace-with-a-private-shared-secret"
```

Run the agent:

```powershell
go run .\cmd\clipsync-windows
```

The client opens a native Windows window. `Alt + V` toggles that window.

On first launch, the Windows client registers itself in the current user's `Run` key. Clipboard images are normalized to PNG, stored as MySQL BLOBs, and can be viewed, copied, and pasted again from the WinUI client. The list loads only the ten most recent matching records and does not load image bodies.

Optional debug web UI for development remains available by calling `agent.Run` from a separate diagnostic entrypoint; it is not the product client.

### Android Client

The native Android source lives in:

```text
android/
```

Build with Android Studio or a machine with Gradle and Android SDK installed:

```powershell
cd android
gradle :app:assembleDebug
```

## Verification

```powershell
go test ./...
go build ./cmd/clipsync-server
go build ./cmd/clipsync-windows
```

Run the isolated MySQL migration test (it creates and removes a random `clipsync_test_*` database):

```powershell
$env:CLIPSYNC_MYSQL_INTEGRATION = "1"
go test ./internal/storage -run TestMySQLLegacyMigrationsAndBinaryImageStorage -count=1 -v
```

## Upgrade Migration

Do not start an upgraded agent against an existing database before migrating its legacy image and queue data. The application enforces this ordering and exits with `legacy ClipSync Pro data migration is required` while old rows remain. First stop the agent and sync server, take and verify a restorable backup, then inspect the read-only plan:

```powershell
go run ./cmd/clipsync-migrate
```

Apply only after checking the plan. The database name confirmation and backup acknowledgement are mandatory safeguards:

```powershell
go run ./cmd/clipsync-migrate --apply --confirm-database=clipsync_pro --backup-confirmed
```

The migration is resumable for images, validates image hashes before clearing legacy text, and atomically compacts the pending queue to one payload-free action per record. Rollback requires stopping the upgraded application and restoring the verified pre-migration backup.

## Documents

- [Architecture](docs/architecture.md)
- [High-Level Design](docs/high-level-design.md)
- [Detailed Design](docs/detailed-design.md)
- [API](docs/api.md)
- [Database Schema](docs/schema.sql)
- [Technical Debt](docs/technical-debt.md)
