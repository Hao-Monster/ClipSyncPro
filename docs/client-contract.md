# ClipSync Pro Client Contract

This document freezes the contract used by the Windows WinUI 3 client, the Android Compose client, and the unchanged Go sync server.

## Runtime Boundary

The server API under `/api/v1` remains unchanged. Client UI rewrites must not change server routes, record field names, encrypted envelope fields, or sync event names.

Windows uses a local Go agent as the native integration boundary:

```text
WinUI 3 UI <-> http://127.0.0.1:37618/api/* <-> Go agent <-> local MySQL + sync server
```

Android talks to the sync server directly and keeps Android-only system integration in Kotlin:

```text
Compose UI <-> Repository <-> HTTP API <-> Go sync server
ForegroundService <-> Android ClipboardManager
```

## Shared Server API

Base URL is configured by the client, defaulting to `http://127.0.0.1:37617`.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Server health. |
| `POST` | `/api/v1/clipboard/records` | Create or update one `ClipboardRecord`. |
| `GET` | `/api/v1/clipboard/records?keyword=&date=&sensitive=` | List records. `date` is `yyyyMMdd`. |
| `GET` | `/api/v1/clipboard/records/search?keyword=` | Search alias. |
| `DELETE` | `/api/v1/clipboard/records/{id}` | Delete one record. |
| `POST` | `/api/v1/clipboard/records/batch-delete` | Delete several records with body `{"ids":[]}`. |
| `GET` | `/ws/v1/sync` | Server-Sent Events stream for sync events. |

## ClipboardRecord

Clients must preserve these JSON field names:

```json
{
  "id": "rec_20260601150000_...",
  "user_id": "local",
  "content_type": "text",
  "content_text": "plain text when not sensitive",
  "content_cipher": null,
  "content_hash": "sha256 hex",
  "is_sensitive": false,
  "sensitive_type": "",
  "account_cipher": null,
  "password_cipher": null,
  "remark_cipher": null,
  "search_index": [],
  "source_device_id": "windows-host",
  "source_platform": "windows",
  "client_created_at": "2026-06-01T15:00:00Z",
  "server_created_at": "2026-06-01T15:00:01Z",
  "updated_at": "2026-06-01T15:00:00Z",
  "deleted_at": null,
  "sync_status": "pending"
}
```

Allowed values:

- `content_type`: `text`, `url`, `number`, `image`, `other`
- `sensitive_type`: empty, `account`, `password`
- `source_platform`: `windows`, `android`, `server`
- `sync_status`: `pending`, `syncing`, `synced`, `failed`

## Encryption Envelope

Sensitive fields use AES-256-GCM with PBKDF2-derived keys. The server stores ciphertext only.

```json
{
  "algorithm": "AES-256-GCM",
  "kdf": "PBKDF2-HMAC-SHA256",
  "iterations": 120000,
  "salt": "base64",
  "nonce": "base64",
  "ciphertext": "base64"
}
```

When a record is marked as an account or password, these fields are encrypted together as one sensitive set:

- `content_cipher`
- `account_cipher`
- `password_cipher`
- `remark_cipher`

The reveal shape is local-client only:

```json
{
  "record_id": "rec_...",
  "content": "original clipboard content",
  "account": "account value",
  "password": "password value",
  "remark": "remark value",
  "expires": "2026-06-01T15:30:00+08:00"
}
```

## Windows Local Agent API

Default base URL: `http://127.0.0.1:37618`.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/api/status` | Agent status, device info, server URL, API version. |
| `GET` | `/api/events` | Local SSE stream for UI refresh. |
| `GET` | `/api/records?keyword=&date=&sensitive=` | List local records for UI. |
| `GET` | `/api/records/{id}/content` | Return the binary body of an image record for preview. |
| `POST` | `/api/records/{id}/reveal` | Reveal one record for 30 minutes in agent cache. |
| `POST` | `/api/records/{id}/mark-password` | Mark selected content as password. Body: `{"account":"","remark":""}`. |
| `POST` | `/api/records/{id}/mark-account` | Mark selected content as account. Body: `{"password":"","remark":""}`. |
| `POST` | `/api/records/{id}/remark` | Update sensitive remark. Body: `{"remark":""}`. |
| `POST` | `/api/records/{id}/copy` | Copy one field. Body: `{"field":"content|account|password|remark"}`. |
| `POST` | `/api/records/{id}/paste` | Paste one field into the active app. |
| `DELETE` | `/api/records/{id}` | Delete one record. UI must confirm sensitive records. |
| `POST` | `/api/records/batch-delete` | Delete many. Body: `{"ids":[],"skip_sensitive":true}`. |
| `POST` | `/api/clipboard/capture-current` | Manually capture current supported Windows clipboard text or image. |

For an image record, the shared `ClipboardRecord.content_text` value is a base64 PNG/BMP data URL. The local list view omits that large value and instead returns `image_url` for lazy preview loading.

Windows UI must not display decrypted values unless the user explicitly clicks reveal. Decrypted values are cached only in agent memory and expire.

## Sync Events

Server and local agent events use this shape:

```json
{
  "event_type": "record_created",
  "record": {},
  "record_id": "",
  "occurred_at": "2026-06-01T15:00:00Z",
  "device_id": "windows-host",
  "request_id": ""
}
```

Allowed `event_type` values:

- `record_created`
- `record_updated`
- `record_deleted`
