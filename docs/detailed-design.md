# Detailed Design

## Record Model

`ClipboardRecord` keeps plaintext only for non-sensitive records. Once a record is marked as account/password data, the original clipboard content is moved into `content_cipher`, and `content_text` is cleared.

When a clipboard value is marked as `account`, that original clipboard value becomes the decrypted `account`, and the user-supplied value becomes the decrypted `password`. When the user clicks view, both fields are decrypted together and shown together. The same applies in reverse for `password`: the original clipboard value becomes `password`, and the user-supplied value becomes `account`.

Important fields:

- `id`: globally unique record id.
- `content_type`: `text`, `url`, `number`, `image`, or `other`.
- `content_text`: non-sensitive plaintext; image rows keep this column empty after migration.
- `image_mime`, `image_blob`: normalized PNG metadata and bytes. Base64 is created only at the single-record compatibility API boundary.
- `content_cipher`: encrypted original sensitive clipboard content.
- `account_cipher`, `password_cipher`, `remark_cipher`: encrypted sensitive fields.
- `search_index`: keyed HMAC token hashes for sensitive search.
- `sync_status`: `pending`, `syncing`, `synced`, or `failed`.

## Encryption

Each encrypted field is an envelope:

```json
{
  "algorithm": "AES-256-GCM",
  "kdf": "PBKDF2-HMAC-SHA256",
  "iterations": 120000,
  "salt": "base64...",
  "nonce": "base64...",
  "ciphertext": "base64..."
}
```

The MVP derives a 256-bit key from `CLIPSYNC_SECRET` and a per-field random salt.

## Sensitive Search Index

Because the server must not store sensitive plaintext, the client computes keyed hashes for lowercase rune n-grams and whole terms. The server can compare query token hashes without learning the original sensitive values. This supports practical contains-style matching while avoiding plaintext storage.

## Date Filter

Date filters accept only `yyyyMMdd`. Invalid dates such as `20260230`, `2026-06-01`, or `abc20260601` are rejected before querying.

## Delete Semantics

The confirmed PRD notes physical deletion. The MVP physically deletes records from `clipboard_record` and keeps delete sync intent in `sync_queue` until the server acknowledges it. The server broadcasts delete events so other clients can remove the record.

## Sync Queue

The active queue is `sync_queue_v2`. It stores only a record id and the latest action for that record; it does not persist record JSON or image Base64. Actions:

- `create`
- `update`
- `delete`

Enqueue uses a unique `record_id` to coalesce repeated changes. The worker materializes the current record immediately before sending, processes records serially, limits each pass by item count and 16 MiB of encoded data, and applies capped exponential retry backoff. Successful rows are deleted rather than retained indefinitely.

Retry state:

- `pending`
- `syncing`
- `done`
- `failed`

The worker is deliberately independent from clipboard capture so server failures do not block local recording.

## Windows System Integration

The Go MVP uses Win32 APIs directly for:

- `GetClipboardSequenceNumber`
- `OpenClipboard`
- `GetClipboardData`
- `SetClipboardData`
- `RegisterHotKey`
- `SendInput`

PNG and supported 24/32-bit packed DIB/DIBV5 clipboard images are normalized to PNG records. Copy/paste restores both standard DIB and PNG clipboard formats when possible. The Windows executables also maintain an idempotent `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` entry, which does not require elevation.

Agent-originated clipboard writes are serialized with polling and tagged by the exact Windows clipboard sequence number. Only that sequence is suppressed once; a later identical value copied by another application remains a new record. Android uses an internal clip label for the equivalent app-originated text writes.

`Alt + V` toggles the native Win32 client window. The default product entrypoint does not require a browser or embedded web view.
