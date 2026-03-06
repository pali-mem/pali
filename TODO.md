# TODO Checklist

> Ordered by roadmap priority and release impact.

## v0.1 Release Gate (Blocking)

- [x] Finalize `tier=auto` behavior for v0.1: auto now resolves to episodic/semantic at store time using deterministic heuristics.
- [x] Align docs to runtime reality before tagging v0.1 (`README.md`, `instructions.md`, `docs/api.md`): remove/mark planned items that are not implemented yet.
- [x] Resolve backend/scorer wiring mismatch: runtime now uses config-driven selection for vector backend and importance scorer, with fail-fast errors for backends not yet implemented.
- [x] Add and document one release smoke command (`make test && make test-integration && make test-e2e && make build`) as the minimum v0.1 go/no-go gate.

## Foundation Completed

- [x] Implement SQLite migrations + real queries (`internal/repository/sqlite`)
- [x] Wire core services into API handlers (replace 501)
- [x] Add request validation + proper error mapping (400/404/409/500)
- [x] Add tenant isolation checks in every memory path
- [x] Add integration tests for memory/tenant/WMR flows
- [x] Implement retrieval pipeline (vector candidate + WMR reranking)
- [x] Add auth toggle + bearer middleware wiring from config
- [x] Add dashboard routes/pages for memories and tenants
- [x] MCP server tool wiring (initial toolset)
- [x] Add MCP `pali_capabilities` tool + tenant fallback resolver (`input -> JWT -> session -> config`)
- [x] Go client in `pkg/client`
- [x] Add benchmark harness + fixture generation scripts
- [x] Consolidate fixture generation to Ollama-only path + parallel `genfix` workers
- [x] Add retrieval quality harness for `/v1/memory/search` (`Top1HitRate`, `Recall@k`, `nDCG@k`, `MRR`, `HitRate@k`)
- [x] Add SQLite write-throughput pragmas and change log (`docs/changes/`)
- [x] Make `embedding.provider=onnx` strict (no mock fallback; fail fast when unavailable)
- [x] Add `cmd/setup` ONNX Runtime shared-library check with platform install hints
- [x] Add Ollama embedder provider with startup readiness checks (`/api/version`, `/api/tags`)
- [x] Make Ollama the default embedding provider; keep ONNX as advanced opt-in
- [x] Add advanced ONNX setup doc for macOS/Windows/Linux (`docs/onnx.md`)

## v0.1 Must Finish

- [x] Implement real ONNX Runtime inference path for `embedding.provider=onnx`
- [x] Replace scaffold e2e tests with real REST and MCP end-to-end scenarios
- [x] Add API support for search filters from spec (`min_score`, tier filters)
- [x] Add memory provenance fields + storage (`source`, `created_by`, `recall_count`, `last_recalled_at`)
- [x] Align docs/API/examples with actual runtime behavior and supported fields
- [x] Curate larger labeled retrieval eval set (query -> expected IDs) and track trend per change
- [x] Production MCP binary entrypoint documented and wired (`pali mcp run`)

## v0.2 Tenant Tooling

- [ ] Expand tenant stats beyond total count (tier counts, top tags, recall stats)
- [ ] Add stronger tenant admin flows in dashboard/API

## v0.3 Backends

- [x] Implement Qdrant vector store adapter and wiring by `vector_backend`
- [ ] Implement pgvector vector store adapter and wiring by `vector_backend`
- [ ] Add backend-specific integration tests and benchmark runs

## v0.4 Scoring

- [x] Implement Ollama scorer `Score` path and request/timeout handling
- [x] Wire config-based scorer selection (heuristic vs ollama) end-to-end
- [ ] Benchmark scorer quality/latency tradeoffs

## v0.5 Dashboard V2

- [ ] Add pagination and richer filtering across memories/tenants
- [ ] Add inline edit/pin flows for memory management
- [ ] Add recall/history visibility in UI

## v0.6 Retrieval Controls

- [ ] Add per-tenant WMR weight tuning
- [ ] Add optional consolidation/promotion jobs for episodic -> semantic

## Performance Track

- [ ] Add explicit transaction batching path for bulk store operations
- [ ] Evaluate embedding storage as binary blob (instead of JSON text)
- [ ] Address linear-scan vector search limitations as tenant memory counts grow
- [ ] Add repeated benchmark matrix (1k/10k/100k + medians) and capture in `docs/changes/`

## v1.0 Readiness

- [ ] Stabilize API contracts and versioning guarantees
- [ ] Tune defaults based on benchmark evidence
- [ ] Validate Linux/macOS/Windows support and setup experience
