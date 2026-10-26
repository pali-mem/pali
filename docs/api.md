# API

Base endpoints:
- `GET /health`
- `POST /v1/tenants`
- `GET /v1/tenants`
- `GET /v1/tenants/:id/stats`
- `POST /v1/memory`
- `GET /v1/memory?tenant_id=...`
- `GET /v1/memory/:id?tenant_id=...`
- `PATCH /v1/memory/:id?tenant_id=...`
- `POST /v1/memory/batch`
- `POST /v1/memory/ingest`
- `POST /v1/memory/ingest/batch`
- `POST /v1/memory/search`
- `GET /v1/memory/jobs/:id`
- `GET /v1/memory/jobs?tenant_id=...`
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
- `GET /v1/memory`, `GET /v1/memory/:id`, `PATCH /v1/memory/:id`, and `DELETE /v1/memory/:id` are tenant-scoped: `tenant_id` is a required query parameter, and an unknown tenant, unknown memory, or a memory that belongs to another tenant returns `404`.
- When `auth.enabled=true`, JWT `tenant_id` must match the request tenant. `GET /v1/tenants` returns only the caller's own tenant for a tenant-scoped token.
- One JWT currently acts as one tenant; there is no multi-tenant admin token mode on these handlers.

Related guide:
- [`multitenancy.md`](multitenancy.md)

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
  "retrieval_kind": "auto",
  "tiers": ["semantic"],
  "kinds": ["observation", "event"],
  "disable_touch": false,
  "debug": false
}
```

## Async ingest memory

`POST /v1/memory/ingest`

Request uses the same schema as `POST /v1/memory`.

Response (`202`):
```json
{
  "ingest_id": "ing_abcd1234",
  "memory_ids": ["mem_abc123"],
  "job_ids": ["ppj_1", "ppj_2"],
  "accepted_at": "2026-03-08T10:00:00Z"
}
```

## Async ingest memory batch

`POST /v1/memory/ingest/batch`

Request uses the same schema as `POST /v1/memory/batch`.

Response (`202`):
```json
{
  "ingest_id": "ing_abcd1234",
  "memory_ids": ["mem_abc123", "mem_def456"],
  "job_ids": ["ppj_1", "ppj_2", "ppj_3"],
  "accepted_at": "2026-03-08T10:00:00Z"
}
```

## Postprocess jobs

`GET /v1/memory/jobs/:id`

Response (`200`):
```json
{
  "id": "ppj_1",
  "ingest_id": "ing_abcd1234",
  "tenant_id": "tenant_1",
  "memory_id": "mem_abc123",
  "type": "vector_upsert",
  "status": "succeeded",
  "attempts": 0,
  "max_attempts": 5,
  "available_at": "2026-03-08T10:00:00Z",
  "created_at": "2026-03-08T10:00:00Z",
  "updated_at": "2026-03-08T10:00:00Z"
}
```

`GET /v1/memory/jobs?tenant_id=<tenant>&status=queued&type=vector_upsert&limit=50`

Supported filters:
- `tenant_id` (required)
- `status`: `queued | running | succeeded | failed | dead_letter` (repeatable or CSV)
- `type`: `parser_extract | vector_upsert` (repeatable or CSV)
- `limit`: positive integer (max 200)

Notes:
- `top_k` defaults to `10` when `<= 0`
- `min_score` must be within `[0,1]`
- `retrieval_kind`: `auto | vector | entity` (default `auto`)
- `tiers` may include `working`, `episodic`, `semantic`
- `kinds` may include `raw_turn`, `observation`, `summary`, `event`
- `disable_touch` skips recall metadata updates for this query (useful for eval/benchmark runs)
- `debug=true` includes planner/ranking diagnostics under `debug`
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
      "importance": 0.5,
      "recall_count": 3,
      "created_at": "2026-03-05T04:00:00Z",
      "updated_at": "2026-03-05T04:20:00Z",
      "last_accessed_at": "2026-03-05T04:20:00Z",
      "last_recalled_at": "2026-03-05T04:20:00Z"
    }
  ]
}
```

## Get memory

`GET /v1/memory/:id?tenant_id=<tenant>`

Response (`200`) is a single memory object with the same fields as a search result item (`id`, `tenant_id`, `content`, `tier`, `kind`, `tags`, `source`, `created_by`, `importance`, `recall_count`, timestamps, and `answer_metadata` when present). Fetching does not update recall metadata.

Errors: `400` when `tenant_id` is missing, `404` when the tenant or memory does not exist in that tenant.

## List memories

`GET /v1/memory?tenant_id=<tenant>&limit=50&offset=0&tier=semantic&kind=observation`

Returns one page of the tenant's memories ordered by creation time, newest first.

Query parameters:
- `tenant_id` (required)
- `limit`: positive integer, default `50`, values above `200` are capped at `200`
- `offset`: non-negative integer, default `0`
- `tier`: `working | episodic | semantic` (repeatable or CSV)
- `kind`: `raw_turn | observation | summary | event` (repeatable or CSV)

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
      "importance": 0.5,
      "recall_count": 3,
      "created_at": "2026-03-05T04:00:00Z",
      "updated_at": "2026-03-05T04:20:00Z",
      "last_accessed_at": "2026-03-05T04:20:00Z",
      "last_recalled_at": "2026-03-05T04:20:00Z"
    }
  ],
  "next_offset": 50
}
```

`next_offset` is the `offset` to request for the next page and is omitted on the last page. Invalid `limit`, `offset`, `tier`, or `kind` values return `400`.

## Update memory

`PATCH /v1/memory/:id?tenant_id=<tenant>`

Request (any non-empty subset of the fields):
```json
{
  "content": "User prefers green tea over coffee.",
  "tags": ["preferences", "drinks"],
  "tier": "semantic",
  "importance": 0.8
}
```

Notes:
- An empty body (`{}`) or a body with none of the four fields returns `400`.
- `content` must not be blank. `tags` replaces the whole tag list; `[]` clears it.
- `tier`: `auto | working | episodic | semantic`, validated like `POST /v1/memory`. `auto` resolves to `semantic` or `episodic` from the updated content and tags.
- `importance` must be within `[0,1]`. It is not rescored when `content` changes.
- Changing `content` rewrites the full-text index and re-embeds the memory synchronously, so the configured vector backend (sqlite, qdrant, or pgvector) stays consistent. If the vector write fails after the row was saved, the response is `500`; retrying the same request re-embeds and repairs the index.
- For `raw_turn` memories with the default identity, the dedupe key follows the new content.
- Changing `content` removes the memory's own entity facts. For a `raw_turn`, when the parser (or structured dual-write) is enabled, extraction runs again on the new content in the same request, the same way `POST /v1/memory` does, and observations/events derived from the old content that the new content does not produce again are deleted with their entity facts. A derived memory with the same entity/relation/value is kept. With an LLM parser this makes the request as slow as a store.
- Updating only `tags`, `tier`, or `importance` leaves derived memories and entity facts unchanged.
- `created_at`, `kind`, `source`, and `created_by` cannot be changed.

Response (`200`) is the updated memory object.

## Delete memory

`DELETE /v1/memory/:id?tenant_id=<tenant>`

Response:
- `204` on success
- `404` when memory does not exist in that tenant

## List tenants

`GET /v1/tenants?limit=100`

`limit` defaults to `100` and is capped at `500`. Tenants are ordered by creation time, newest first.

Response (`200`):
```json
{
  "items": [
    {
      "id": "tenant_1",
      "name": "Tenant One",
      "created_at": "2026-03-05T03:00:00Z",
      "memory_count": 42
    }
  ]
}
```

With `auth.enabled=true` a tenant-scoped token receives only its own tenant.

## Tenant stats

`GET /v1/tenants/:id/stats`

Response (`200`):
```json
{
  "tenant_id": "tenant_1",
  "memory_count": 42,
  "by_tier": {"semantic": 10, "episodic": 32},
  "by_kind": {"raw_turn": 30, "observation": 12},
  "total_recalls": 87,
  "newest_memory_at": "2026-03-05T04:00:00Z"
}
```

Notes:
- `memory_count` is unchanged from earlier releases; the other fields are additive.
- `by_tier` and `by_kind` only contain keys with at least one memory (empty objects for an empty tenant).
- `total_recalls` is the sum of `recall_count` across the tenant's memories.
- `newest_memory_at` is the latest memory `created_at`, or `null` for an empty tenant.
