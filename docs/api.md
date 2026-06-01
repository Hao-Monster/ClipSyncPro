# API

Base path:

```text
/api/v1
```

## Create or Update Record

```http
POST /api/v1/clipboard/records
```

Body: `ClipboardRecord` JSON.

## List Records

```http
GET /api/v1/clipboard/records?keyword=abc&date=20260601&sensitive=true
```

Query parameters:

- `keyword`: optional contains search.
- `date`: optional `yyyyMMdd`.
- `sensitive`: optional `true`.

## Search Alias

```http
GET /api/v1/clipboard/records/search?keyword=abc
```

## Delete Record

```http
DELETE /api/v1/clipboard/records/{id}
```

## Batch Delete

```http
POST /api/v1/clipboard/records/batch-delete
```

```json
{
  "ids": ["id1", "id2"]
}
```

## Sync Events

```http
GET /ws/v1/sync
```

The MVP uses Server-Sent Events:

```text
event: sync
data: {"event_type":"record_created","record":{...}}
```

Event types:

- `record_created`
- `record_updated`
- `record_deleted`
