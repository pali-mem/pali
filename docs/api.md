# API

Base endpoints:
- `GET /health`
- `POST /v1/tenants`
- `GET /v1/tenants/:id/stats`
- `POST /v1/memory`
- `POST /v1/memory/batch`
- `POST /v1/memory/search`
- `DELETE /v1/memory/:id?tenant_id=...`

Error mapping:
- `400` invalid input / malformed JSON
- `401` missing or invalid JWT (when auth is enabled)
- `403` tenant mismatch (JWT tenant vs request tenant)
- `404` missing tenant or memory
- `409` conflict (duplicate IDs / constraints)
- `500` internal error

Tenant isolation:
- Memory operations require a valid tenant.
- `POST /v1/memory`, `POST /v1/memory/batch`, and `POST /v1/memory/search` return `404` when tenant does not exist.
- `DELETE /v1/memory/:id` is tenant-scoped.

## Store memory

`POST /v1/memory`

Request:
```json
{
  "tenant_id": "tenant_1",
  "content": "User prefers tea over coffee.",
  "tier": "semantic",
  "kind": "raw_turn",
  "tags": ["preferences"],
  "source": "chat_message",
  "created_by": "user"
}
```

Notes:
- `tier`: `auto | working | episodic | semantic` (default `auto` when omitted/empty)
- `kind`: `raw_turn | observation | summary | event` (default `raw_turn`)
- when `tier=auto`, server resolves the stored tier to `semantic` or `episodic` at write time using deterministic heuristics
- `created_by`: `auto | user | system` (default `auto`)
- `source` is optional free text for provenance

Response (`201`):
```json
{
  "id": "mem_abc123",
  "created_at": "2026-03-05T04:00:00Z"
}
```

## Store memory batch

`POST /v1/memory/batch`

Request:
```json
{
  "items": [
    {
      "tenant_id": "tenant_1",
      "content": "User prefers tea over coffee.",
      "tier": "semantic",
      "kind": "raw_turn",
      "tags": ["preferences"],
      "source": "chat_message",
      "created_by": "user"
    },
    {
      "tenant_id": "tenant_1",
      "content": "User moved to Austin in 2024.",
      "tier": "episodic",
      "kind": "event",
      "tags": ["profile"],
      "source": "chat_message",
      "created_by": "system"
    }
  ]
}
```

Notes:
- `items` must be non-empty.
- Each item uses the same schema/rules as `POST /v1/memory`.
- Writes are processed in request order.

Response (`201`):
```json
{
  "items": [
    {
      "id": "mem_abc123",
      "created_at": "2026-03-05T04:00:00Z"
    },
    {
      "id": "mem_def456",
      "created_at": "2026-03-05T04:00:00Z"
    }
  ]
}
```

## Search memory

`POST /v1/memory/search`

Request:
```json
{
  "tenant_id": "tenant_1",
  "query": "tea preference",
  "top_k": 10,
  "min_score": 0.25,
  "tiers": ["semantic"],
  "kinds": ["observation", "event"],
  "disable_touch": false
}
```

Notes:
- `top_k` defaults to `10` when `<= 0`
- `min_score` must be within `[0,1]`
- `tiers` may include `working`, `episodic`, `semantic`
- `kinds` may include `raw_turn`, `observation`, `summary`, `event`
- `disable_touch` skips recall metadata updates for this query (useful for eval/benchmark runs)
- returned `tier` values are persisted tiers (`working|episodic|semantic`); `auto` is not returned

Response (`200`):
```json
{
  "items": [
    {
      "id": "mem_abc123",
      "tenant_id": "tenant_1",
      "content": "User prefers tea over coffee.",
      "tier": "semantic",
      "kind": "raw_turn",
      "tags": ["preferences"],
      "source": "chat_message",
      "created_by": "user",
      "recall_count": 3,
      "created_at": "2026-03-05T04:00:00Z",
      "updated_at": "2026-03-05T04:20:00Z",
      "last_accessed_at": "2026-03-05T04:20:00Z",
      "last_recalled_at": "2026-03-05T04:20:00Z"
    }
  ]
}
```

## Delete memory

`DELETE /v1/memory/:id?tenant_id=<tenant>`

Response:
- `204` on success
- `404` when memory does not exist in that tenant
