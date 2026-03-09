# Pali Runtime Instructions

This file is the runtime-aligned product/engineering snapshot for this repository.
If this file and code disagree, code + tests are the source of truth.

## v0.1 Scope (Implemented)

Pali is a local memory service for LLM apps with:
- REST API server (`cmd/pali`)
- MCP server over stdio (`pali mcp run`)
- Minimal dashboard for tenant/memory operations
- SQLite metadata + sqlite-based vector search backend
- Hybrid retrieval (lexical + dense fusion) + WMR reranking
- Multi-tenant isolation with optional JWT auth

## REST API (Implemented)

Base routes:
- `GET /health`
- `POST /v1/tenants`
- `GET /v1/tenants/:id/stats`
- `POST /v1/memory`
- `POST /v1/memory/batch`
- `POST /v1/memory/search`
- `DELETE /v1/memory/:id?tenant_id=...`

Dashboard routes:
- `GET /dashboard`
- `GET /dashboard/memories`
- `POST /dashboard/memories`
- `POST /dashboard/memories/:id/delete`
- `GET /dashboard/tenants`
- `POST /dashboard/tenants`
- `GET /dashboard/stats`

Full request/response details live in `docs/api.md`.

## MCP Tools (Implemented)

Current MCP tool names:
- `memory_store`
- `memory_store_preference`
- `memory_search`
- `memory_list`
- `memory_delete`
- `tenant_create`
- `tenant_list`
- `tenant_stats`
- `tenant_exists`
- `health_check`
- `pali_capabilities`

Tenant resolution order for tenant-aware tools:
1. explicit `tenant_id` in tool input
2. JWT tenant claim (when auth is enabled and transport provides token)
3. MCP session default tenant
4. `default_tenant_id` from config
5. otherwise error

## Memory Model + Retrieval Behavior

Supported tiers:
- `working`
- `episodic`
- `semantic`
- `auto` (input-only convenience)

`tier=auto` policy at store time:
- explicit non-auto tiers are preserved
- auto resolves to `semantic` when stable signals are detected:
  - `created_by=user` or `created_by=system`
  - semantic tags (for example: `preferences`, `profile`, `always`)
  - preference/profile-like phrases in content (for example: `user prefers`, `i prefer`, `my name is`)
- otherwise auto resolves to `episodic`

Search behavior:
- lexical candidates + dense candidates are fused via Reciprocal Rank Fusion (`k=60`)
- final ranking uses WMR-style score from recency/relevance/importance
- successful search touches returned memories (`last_accessed_at`, `last_recalled_at`, `recall_count`)

## Config Surface (Implemented)

Canonical config reference:
- `docs/configuration.md` (authoritative)
- `pali.yaml.example` (must stay aligned with `internal/config/defaults.go`)

Runtime notes:
- `vector_backend=sqlite` is implemented.
- `vector_backend=qdrant` is implemented using Qdrant HTTP API (`collections`, `points upsert/search/delete`) with tenant payload filters.
- `vector_backend=pgvector` currently returns fail-fast startup errors (adapter not implemented yet).
- `entity_fact_backend=sqlite` is implemented.
- `entity_fact_backend=neo4j` is implemented with batch-first UNWIND writes and relation lookup queries.
- `importance_scorer=heuristic` is default.
- `importance_scorer=ollama` calls local Ollama for score generation.
- `importance_scorer=openrouter` calls OpenRouter chat completions for score generation.
- `postprocess.enabled=true` runs in-process workers for async ingest queue jobs (`parser_extract`, `vector_upsert`).
- default embedding provider is `ollama` with `lexical` fallback.
- `embedding.provider=openrouter` calls OpenRouter embeddings API.
- ONNX embedding path is implemented and requires ONNX Runtime shared library.
- benchmark/eval scripts render runtime config from `test/config/providers/*.yaml` via `cmd/configrender`.

## Setup + Run + Validation

Setup:
```bash
make setup
```
Config reference: `docs/configuration.md`

Run API:
```bash
make run
# or
go run ./cmd/pali -config pali.yaml
```

Run MCP:
```bash
make mcp
# or
go run ./cmd/pali mcp run -config pali.yaml
```

Release smoke gate:
```bash
make test && make test-integration && make test-e2e && make build
```

## Known Gaps (Not Yet Implemented)

- pgvector adapter + runtime wiring
- richer tenant stats (tier/tag/recall breakdowns)
- dashboard v2 features (pagination, inline edit/pin, recall history)
- per-tenant WMR weight tuning

These are tracked in `TODO.md` and associated docs in `docs/changes/`.
