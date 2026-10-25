# Per-Tenant Scoring Weights (design proposal, 2026-10-25)

Status: proposal, not implemented. Listed as a known gap in `instructions.md`.

## Why

Ranking weights (`retrieval.scoring.wal` / `retrieval.scoring.match`) are global config. A server hosting
several kinds of tenants (a coding agent's project memory, a chat assistant's user memory) cannot tune
recency vs relevance per tenant without running separate servers.

## Scope

- Per-tenant override of `retrieval.scoring.algorithm` and its weights only.
- Not in scope: per-tenant embedding provider, parser settings, or candidate window tuning. Those change what
  is stored or indexed and need migration logic; scoring weights only change read-time math.

## Design

### Storage

New table, created by the SQLite migration list (`internal/repository/sqlite/migrations.go`):

```sql
CREATE TABLE IF NOT EXISTS tenant_settings (
  tenant_id TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
  settings_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

`settings_json` holds a versioned object so later per-tenant settings (for example observational memory on/off)
can share the table:

```json
{ "version": 1, "scoring": { "algorithm": "wal", "wal": { "recency": 0.3, "relevance": 0.6, "importance": 0.1 } } }
```

### Resolution

At search time: tenant override if present, else global config. Missing fields inside an override fall back
field by field to the global value. Validation is the same as config validation (weights >= 0, algorithm in
`wal|match`); invalid overrides are rejected at write time, never at read time.

Read cost: one primary-key lookup per search, cached in memory per tenant with invalidation on write
(single-process server, so a map + mutex is enough).

### API

- `GET /v1/tenants/:id/settings` returns effective settings and which fields are overridden.
- `PUT /v1/tenants/:id/settings` replaces the override object; `DELETE` removes it.
- JWT: same tenant-claim rule as other tenant routes.
- Dashboard: read-only display of effective weights on the tenant page in v1.

### Code touch points

- `internal/core/memory/search*.go`: `rankMemories` currently receives `s.ranking`; it would receive the
  resolved per-tenant `RankingOptions` instead. No change to the scoring math.
- New `internal/core/tenant/settings.go` service + repository methods.

## Testing

- Resolution unit tests (no override, partial override, full override).
- Integration: two tenants with opposite recency weights, same stored data, assert opposite ordering.
- Auth tests for the new routes.

## Open question (maintainer decision)

Do real users need this before the coding-agent integration ships? If not, defer; the table is still the
right home for the observational-memory per-tenant switch if that is chosen.
