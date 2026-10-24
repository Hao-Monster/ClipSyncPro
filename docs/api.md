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

Image bodies remain Base64 data URLs on this compatibility endpoint. They are converted to BLOBs at the storage boundary.

## List Records

```http
GET /api/v1/clipboard/records?keyword=abc&date=20260601&sensitive=true
```

Query parameters:

- `keyword`: optional contains search.
- `date`: optional `yyyyMMdd`.
- `sensitive`: optional `true`.

The response contains at most the ten most recent matching records. Image records are summaries and do not include `content_text`.

## Get One Record

```http
GET /api/v1/clipboard/records/{id}
```

This endpoint returns the requested full record and is the only record read endpoint that includes an image body.

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
data: {"event_type":"record_created","record_id":"rec_..."}
```

Events are invalidation notifications rather than record transport. Clients debounce bursts and fetch a single record by id when required.

Event types:

- `record_created`
- `record_updated`
- `record_deleted`
