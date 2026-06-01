# High-Level Design

## Modules

| Module | Responsibility |
| --- | --- |
| `domain` | Clipboard record, encrypted field, sync event, date formatting |
| `security` | AES-256-GCM, PBKDF2-HMAC-SHA256, keyed sensitive search index |
| `storage` | Store interface, in-memory implementation, MySQL implementation |
| `app` | Use cases: capture, search, mark sensitive, reveal, delete |
| `httpapi` | Server API and event stream |
| `syncer` | Client-side sync queue flushing and event subscription |
| `agent` | Windows background runtime: clipboard capture, sync queue, event subscription |
| `nativeui` | High-performance Win32 native desktop UI |
| `win32` | Windows clipboard, hotkey, and paste primitives |
| `android` | Kotlin native Android client source |

## Windows Flow

1. Clipboard sequence number changes.
2. Agent reads Unicode text from Windows clipboard.
3. App service creates a `ClipboardRecord`.
4. Store writes the record to local MySQL.
5. Store appends `sync_queue(create)`.
6. Sync worker posts the record to the server.
7. Server acknowledges and broadcasts `record_created`.

## Android Flow

1. Android captures clipboard while allowed by the OS.
2. Android posts directly to the server.
3. Server stores and broadcasts the event.
4. Windows receives the event stream and writes the record into local MySQL.

## Native UI Flow

The Windows product UI is a native Win32 window launched by `cmd/clipsync-windows`. The old local web handler is retained only as a debug surface and is not the default client.

It supports:

- all records
- sensitive records
- keyword search
- `yyyyMMdd` date filter
- single delete
- batch delete over current result set
- password/account marking
- reveal sensitive data with a 30-minute in-memory cache
- copy and paste actions
- `Alt + V` native window toggle

## Data Ownership

Windows local MySQL is the fast local source of truth for the native Windows UI. The server is the cross-device sync source. Android does not persist records locally.

## MVP Scope

Implemented:

- text clipboard capture on Windows
- local MySQL schema and store
- record APIs
- sync queue
- sensitive encryption and reveal
- sensitive search hash generation
- Win32 native Windows UI
- Windows clipboard read/write and paste primitives
- Android Kotlin Activity and foreground clipboard service source

Planned next:

- binary/image/file clipboard payload adapters
- production MySQL server store
- tray icon menu and installer task-scheduler autostart
- Android accessibility/input-method capture variants
