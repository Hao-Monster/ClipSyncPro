# ClipSync Pro

ClipSync Pro is a private-deployment clipboard history and cross-device sync system for Windows and Android.

This repository currently contains a Go-based MVP foundation that can be built in the current workspace:

- `cmd/clipsync-server`: HTTP sync server with record APIs and Server-Sent Events at the PRD-compatible `/ws/v1/sync` path.
- `cmd/clipsync-windows`: Windows local agent with clipboard polling, local MySQL storage, sync queue, `Alt + V` hotkey hook, and a local web UI.
- `internal/domain`: shared record and sync event model.
- `internal/security`: AES-256-GCM encryption and keyed sensitive search indexes.
- `internal/storage`: memory and MySQL storage implementations.

The target product architecture remains Windows native desktop + Android native client + Go sync server. Because Rust/Cargo and Gradle are not available in this environment, the checked-in MVP uses Go end to end for the runnable Windows and server slice.

## Quick Start

### Server

```powershell
go run .\cmd\clipsync-server
```

Default server address:

```text
http://127.0.0.1:37617
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

Local UI:

```text
http://127.0.0.1:37618
```

## Verification

```powershell
go test ./...
go build ./cmd/clipsync-server
go build ./cmd/clipsync-windows
```

## Documents

- [Architecture](docs/architecture.md)
- [High-Level Design](docs/high-level-design.md)
- [Detailed Design](docs/detailed-design.md)
- [API](docs/api.md)
- [Database Schema](docs/schema.sql)
