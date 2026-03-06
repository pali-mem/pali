# SQLite Layer

This project uses SQLite as the default metadata store for tenants and memories.

## Files

- `internal/repository/sqlite/db.go`: opens DB, enables FK, runs migrations
- `internal/repository/sqlite/migrations.go`: schema + indexes
- `internal/repository/sqlite/queries.go`: SQL statements
- `internal/repository/sqlite/tenant.go`: tenant repository implementation
- `internal/repository/sqlite/memory.go`: memory repository implementation

## Connection

Use the helper:

```go
ctx := context.Background()
db, err := sqlite.Open(ctx, "file:pali.db?cache=shared")
```

If DSN is empty, default is:

`file:pali.db?cache=shared`

`Open` will:

1. open the SQLite connection (`modernc.org/sqlite` driver)
2. apply baseline pragmas:
   - `PRAGMA foreign_keys = ON`
   - `PRAGMA cache_size = -64000` (64MB cache target)
   - `PRAGMA temp_store = MEMORY`
3. apply write-throughput pragmas for file-backed DBs:
   - `PRAGMA journal_mode = WAL`
   - `PRAGMA synchronous = NORMAL`
4. run migrations idempotently

Note: in-memory DSNs (`:memory:`, `file::memory:`, or `mode=memory`) skip WAL/synchronous tuning.

## Schema

### `tenants`

- `id` (`TEXT`, PK)
- `name` (`TEXT`, required)
- `created_at` (`TEXT`, RFC3339 timestamp)

### `memories`

- `id` (`TEXT`, PK)
- `tenant_id` (`TEXT`, required, FK -> `tenants.id`)
- `content` (`TEXT`, required)
- `tier` (`TEXT`, required)
- `tags_json` (`TEXT`, JSON array, default `[]`)
- `source` (`TEXT`, default `''`)
- `created_by` (`TEXT`, default `'auto'`; values: `auto|user|system`)
- `importance_score` (`REAL`, default `0`)
- `recall_count` (`INTEGER`, default `0`)
- `created_at` (`TEXT`, RFC3339 timestamp)
- `updated_at` (`TEXT`, RFC3339 timestamp)
- `last_accessed_at` (`TEXT`, RFC3339 timestamp)
- `last_recalled_at` (`TEXT`, RFC3339 timestamp)

### `memory_embeddings`

- `tenant_id` (`TEXT`, part of PK)
- `memory_id` (`TEXT`, part of PK, FK -> `memories.id`)
- `embedding_json` (`TEXT`, serialized float vector)
- `updated_at` (`TEXT`, RFC3339 timestamp)

Indexes:

- `idx_memories_tenant_updated (tenant_id, updated_at DESC)`
- `idx_memories_tenant_created (tenant_id, created_at DESC)`
- `idx_memories_tenant_accessed (tenant_id, last_accessed_at DESC)`

## CRUD Coverage

Current repository methods:

- Tenants:
  - `Create(ctx, tenant)`
  - `Exists(ctx, tenantID)`
  - `MemoryCount(ctx, tenantID)`
- Memories:
  - `Store(ctx, memory)`
  - `Search(ctx, tenantID, query, topK)`
  - `Delete(ctx, tenantID, memoryID)`
  - `GetByIDs(ctx, tenantID, ids)`
  - `Touch(ctx, tenantID, ids)`

`Touch` is used after retrieval and updates:
- `last_accessed_at`
- `last_recalled_at`
- `recall_count = recall_count + 1`

Vector candidate retrieval is implemented in `internal/vectorstore/sqlitevec` using tenant-scoped embeddings and cosine similarity.

## Tests

Repository tests run against in-memory SQLite:

- `internal/repository/sqlite/tenant_test.go`
- `internal/repository/sqlite/memory_test.go`

Run:

```bash
go test ./internal/repository/sqlite -v
```
