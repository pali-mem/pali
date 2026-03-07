# Pali Product Improvement Plan

Date: 2026-03-06

## Executive Summary

Pali is already a real product shape:

- [x] Local persistent memory layer for LLM apps.
- [x] Single Go codebase with REST, MCP, dashboard, and multi-tenant auth.
- [x] Pluggable embedder, vector backend, scorer, parser, and structured-memory options.
- [x] Passing package test suite (`go test ./internal/... ./pkg/...`).

But the current floor is still too low for the plugin story to matter.

The key issue is not "SQLite vs Qdrant" or "MiniLM vs a better embedder". The key issue is that Pali still stores and ranks the wrong memory units, with weak write reliability and a hybrid retrieval stack that throws away useful scoring signal before reranking.

That is why small curated retrieval can look decent while LOCOMO-style quality is still not where it needs to be:

- Small curated retrieval baseline is already above the "20s" if you look at toy Top1:
  - `test/benchmarks/results/20260305T213208Z_sqlite_memories_retrieval_quality.json`: Top1 `0.370968`, Recall@10 `0.864055`, nDCG@10 `0.629185`, MRR `0.567576`.
- The product-level benchmark is the harder one:
  - `research/results/mini_runs/reuse_matrix_20260306T133450Z/ollama_hybrid_qwen7b.summary.txt`: overall F1 `15.17`, Recall@60 `0.313083`, single-hop F1 `3.76`, open-domain F1 `5.56`.

So the plan below treats the toy fixture as a regression harness, not as the main product gate. The real gate should be the paperlite/LOCOMO-style benchmark, and the rule should be:

- [ ] Do not spend serious time on a better model or better vector backend until Pali clears `20.0` overall F1 on the locked paperlite mini benchmark with the current backend and current embedder family.

## What Pali Is Today

Pali today is:

- A Go server started by `make run` or `go run ./cmd/pali -config pali.yaml`.
- A sibling MCP server started by `make mcp` or `go run ./cmd/pali mcp run -config pali.yaml`.
- A configurable runtime built around:
  - `embedding.provider`
  - `vector_backend`
  - `importance_scorer`
  - `parser`
  - `structured_memory`

The main architecture entry points are:

- `cmd/pali/main.go`
- `internal/api/router.go`
- `internal/core/memory/*`
- `internal/repository/sqlite/*`
- `internal/vectorstore/*`
- `internal/embeddings/*`
- `internal/mcp/tools/registry.go`

This plug-and-play shape is good. The problem is that the core memory and retrieval semantics are still too weak, so plugin swaps mostly change the ceiling, not the floor.

## Root Causes From The Codebase

These are the main design flaws visible in the current code, independent of the external reports.

### 1. Write-path consistency is not atomic

The store path writes metadata first and vectors after:

- `internal/core/memory/store.go:161+168` (`storeBatchWithoutParser`: `storeInRepo` then `upsertStoredEmbeddings`)
- `internal/core/memory/store.go:717+724` (`storeOnePrecomputed`: `storeInRepo` then `vector.Upsert`)
- `internal/core/memory/store_batch_parser.go:307+315` (`storeBatchWithParser`: same pattern)

Delete does the same in reverse:

- `internal/core/memory/delete.go:21+25` (`repo.Delete` then `vector.Delete`)

If repo write succeeds and vector upsert/delete fails, Pali can drift into a partially indexed state. There is no outbox, no retry queue, no indexing state, and no repair loop.

### 2. The "sqlitevec" backend is not a serious vector baseline

`internal/vectorstore/sqlitevec/store.go:97-170` (`Search` method) does:

- tenant-wide row scan via `ListEmbeddingsByTenantSQL`
- JSON unmarshal per embedding row
- Go-side cosine on every row
- full sort of all candidates

Note: `sqlitevec` already has `UpsertBatch` (transactional batch insert).
Update: `qdrant/store.go` now implements `UpsertBatch`; remaining work is benchmark parity and repeated-run variance control.

This is not sqlite-vec ANN. It is JSON scan plus Go math. That means current "vector backend" comparisons are mixing product quality issues with an intentionally weak search primitive.

### 3. Hybrid retrieval loses signal before ranking

Current search flow:

- lexical candidates from repo search
- dense candidates from vector search
- RRF to get candidate IDs
- rerank using `similarityByID`

Relevant code:

- `internal/core/memory/search.go:69-115` (candidate gathering + RRF call)
- `internal/core/memory/search.go:309-410` (`rankMemories` — uses `similarityByID` as the relevance signal)
- `internal/core/memory/search.go:489-547` (`fuseCandidatesByRRF` — confirmed at line 489)

The problem is that reranking does not preserve real lexical score features. In `fuseCandidatesByRRF`, the lexical side is stored as `lexicalSignal = 1/(60+rank)` (≈ 0.016 max) in `similarityByID`. For candidates appearing only in lexical results, `similarityByID` is this tiny value, not a real content-match score. `rankMemories` then uses `similarityByID` as the relevance component for all candidates — so lexical-only hits are severely penalized. Candidates in both sets correctly get the dense cosine score (since dense score > lexical RRF signal), but the lexical signal is still lost.

### 4. Candidate generation is shallow and fixed

`internal/core/memory/search.go:478-486` (`candidateWindow` function, confirmed at line 478) clamps candidate window to `50..200` regardless of query hardness, tenant size, or filter use. That is too blunt for a memory system that needs exact-attribute recall.

### 5. Filters are applied late, not during retrieval

Search gets candidates first and only then applies kind/tier/min-score filtering:

- `internal/core/memory/search.go:127-147` (confirmed: filter loop at line 128, `MinScore` check at line 139)

That wastes candidate budget and can drop relevant results when the corpus grows.

### 6. Query routing is mostly regex + score multiplier

Relevant code:

- `internal/core/memory/query_routing.go:10-190` (all regex patterns and `routeBoost` multipliers)
- `internal/core/memory/search.go:109-115` (`classifyQuery` → `rankMemories` with route profile)
- `internal/core/memory/search.go:177-260` (`searchByEntityFacts` — the one narrow aggregation path)

This is not a query planner. It is:

- regex classification
- kind-level boost
- one narrow aggregation path

Even on the aggregation path there is a correctness gap: `min_score` is effectively ignored because `internal/core/memory/search.go:241` checks `opts.MinScore > 1`, which can never be true — the entry-point validation at line 34 rejects any `MinScore` outside `[0, 1]`, making `> 1` always false.

### 7. Memory representation is duplicated and inconsistent

Depending on config, a single turn can become multiple representations:

- raw turn (parser path, if `StoreRawTurn=true`)
- parsed facts + entity facts (parser path)
- legacy structured observations + legacy event projection (non-parser path only, when `DualWriteObservations=true` / `DualWriteEvents=true`)

The legacy dual-write path (`writeLegacyStructuredDerived`) is **only** triggered from `storeBatchWithoutParser`. On the parser path (`storeBatchWithParser`), raw-turn fallbacks go through `storeOnePrecomputed` which does NOT call `writeLegacyStructuredDerived`. So the full four-way duplication only occurs in non-parser mode.

Relevant code:

- `internal/core/memory/store.go:173` (call to `writeLegacyStructuredDerived` in `storeBatchWithoutParser` loop)
- `internal/core/memory/store.go:521-560` (`writeLegacyStructuredDerived` function body — **not** 731-763; lines 731+ are inside `prepareParsedFactsForStore`)
- `internal/core/memory/store_batch_parser.go:46-374` (`storeBatchWithParser` — parser path, entity facts only, no legacy obs/event dual-write)
- `internal/core/memory/structured_observations.go`

Pali does not yet have one canonical retrieval unit. That makes ranking, dedupe, update, and benchmarking unstable.

### 8. Parsed-fact dedupe/update is lexical and history-destroying

Relevant code:

- `internal/core/memory/store.go:399-448` (`applyParsedFact` — sequential path dedupe/update logic; **note:** lines 306-357 are inside `parseFactsWithFallback` error handling, and 359-397 are inside `precomputeParserEmbeddings` — neither is the dedupe logic)
- `internal/core/memory/store.go:451-469` (`findSimilarMemory` — lexical-only similarity used for dedupe decision)
- `internal/core/memory/store.go:505-519` (`deleteForReplacement` — hard delete, no versioning)
- `internal/core/memory/store_batch_parser.go:456-540` (`applyParsedFactWithPending` — batch-path equivalent; `deleteForReplacement` call at line 510)

The system decides dedupe/update by lexical overlap on repo search output. When it decides to replace, it deletes the old memory instead of versioning it. That loses auditability and can hide stale-memory bugs.

### 9. Tier semantics are too broad to be meaningful

`internal/core/memory/tier_policy.go:41-48` (`shouldPromoteToSemantic`) promotes anything where `CreatedBy == user` or `CreatedBy == system` to semantic — confirmed. That collapses tier boundaries fast, especially when parser-derived memories are `system` created (`applyParsedFact` at store.go:399 hardcodes `CreatedBy: domain.MemoryCreatedBySystem`).

### 10. Lifecycle APIs are missing

Routes and DTOs expose only store, batch store, search, and delete:

- `internal/api/router.go:136-143` (confirmed: `POST /memory`, `POST /memory/batch`, `POST /memory/search`, `DELETE /memory/:id` only)
- `internal/api/handlers/memory.go:21-160`
- `internal/api/dto/memory.go:5-56`
- `internal/mcp/tools/registry.go:62-100` (confirmed: `memory_store`, `memory_store_preference`, `memory_search`, `memory_list`, `memory_delete`, `tenant_create`, `tenant_list` — no lifecycle tools)

There is no update, forget, supersede, history, audit, pin, or reindex control path.

### 11. Benchmarks are still easy to game in the default path

`scripts/benchmark.sh` previously only benchmarked sqlite and auto-generated search queries from the first 3 words:

- `scripts/benchmark.sh:145-148` (sqlite-only guard: `if [[ "$BACKEND" != "sqlite" ]]` confirmed)
- `scripts/benchmark.sh:472` (first-3-word query: `awk '{print $1, $2, $3}'` confirmed at line 472)

`scripts/retrieval_quality.sh` previously only supported sqlite:

- `scripts/retrieval_quality.sh:172-174` (same guard: `if [[ "$BACKEND" != "sqlite" ]]` confirmed)

Update (implemented): both scripts now support `sqlite` and `qdrant`, and `benchmark.sh` no longer relies on first-3-word prefix queries as the primary workload.

### 12. Parser evidence can be written but not credited if eval mapping drifts

A concrete failure mode observed in LOCOMO paperlite runs:

- parser/structured rows were written (`source` suffix like `:run_<stamp>:parser`)
- retrieval returned those rows
- but eval grouping only mapped `source LIKE 'eval_row_%:run_<stamp>'` (no trailing `%`)

That under-counts expected evidence groups and can hide or distort the impact of parser work.

Fix (implemented):

- `research/eval_locomo_f1_bleu.py` now maps with `source LIKE 'eval_row_%:run_<stamp>%'` so parser-suffixed rows are included.

Operational rule:

- treat runner/evaluator flag drift as a product risk
- benchmark runner scripts must pass critical retrieval/context flags explicitly (not rely on silent defaults)
- after parser-enabled runs, validate index-map expansion (`avg ids/group > 1` on parser-active fixtures)
- default paperlite iteration budget should be capped (`120-150` queries), not full eval-set count, unless explicitly running a final gate
- evaluator code must not contain dataset-specific question or answer phrase heuristics; no LOCOMO-specific rewrites/bonuses in scoring or extraction

## Priority Plan

The list below is ordered from highest product impact to lowest. The first five items are the real product floor. Items after that matter, but they should not displace the first five.

### [ ] 1. Make source-grounded atomic fact memories the primary retrieval unit

Why this is first:

- Current retrieval quality is bottlenecked by what gets stored, not just how it is ranked.
- Raw turn + derived memory duplication is making exact factual retrieval unstable.
- The product cannot become backend-agnostic until the base memory unit is right.

What to do:

- Store one archival raw turn, but stop treating it as the primary retrieval unit.
- First implementation should not depend on a model-stable cross-turn `canonical_id`.
- Instead, use a deterministic source-grounded identity for each extracted atomic fact:
  - `source_turn_id`
  - `fact_index` or source span
  - `kind`
  - normalized fact text hash
  - extracted `(entity, relation, value)` tuple when available
  - `extractor`
  - `extractor_version`
  - `confidence`
  - `memory_state`
- Preserve one-to-many relation from source turn to atomic memories.
- Add cross-turn consolidation only after extractor quality is measured on labeled cases.
- Replace "legacy structured dual write" with one canonical extraction path.
- Dedupe in phase 1 by source-grounded identity plus relation tuple, not by lexical overlap.
- Only add semantic or paraphrase-based consolidation in a later phase, once extraction reliability is proven.

Files to track:

- `internal/domain/memory.go`
- `internal/repository/sqlite/migrations.go`
- `internal/repository/sqlite/memory.go`
- `internal/core/memory/store.go`
- `internal/core/memory/store_batch_parser.go`
- `internal/core/memory/info_parser_heuristic.go`
- `internal/core/memory/info_parser_ollama.go`
- `internal/core/memory/structured_observations.go`
- `internal/core/memory/entity_facts.go`

Stop gates:

- [ ] Gate C1 core-memory benchmark reaches extractive-only overall F1 `>= 18.0` after this phase.
- [ ] Gate C2 product benchmark remains the promotion gate to model/backend work: overall F1 `>= 20.0`.
- [ ] Single-hop F1 `>= 10.0`.
- [ ] Open-domain F1 `>= 7.0`.
- [ ] Multi-hop F1 `>= 8.0`.
- [ ] Temporal F1 does not regress below `35.0`.

Completion policy for #1:

- [ ] #1 is not treated as complete until all Gate C thresholds above are passing on the locked 120-query run profile.
- [ ] #1 completion requires canonical-memory behavior plus retrieval correctness fixes listed in #4 and #5 where they directly affect canonical-unit recall.

### [ ] 2. Make writes and deletes transactional from Pali's point of view, sync-first

Why this is second:

- Right now Pali can succeed and still leave the retrieval index wrong.
- You cannot trust any benchmark or production behavior when index drift is possible.

What to do:

- Add an outbox/index-jobs table written in the same transaction as the canonical memory rows.
- Introduce explicit indexing states:
  - `pending`
  - `indexed`
  - `failed`
  - `tombstoned`
- For the local single-process product, prefer a synchronous indexing envelope first:
  - write canonical rows plus pending index state in one SQLite transaction
  - try vector/entity indexing inline before returning
  - mark `indexed` or `failed` before the request completes
- Do not introduce an async worker in the first cut unless the synchronous state model is already correct and benchmarked.
- Add a repair/reconcile command that scans for drift.

Files to track:

- `internal/repository/sqlite/migrations.go`
- `internal/repository/sqlite/memory.go`
- `internal/core/memory/store.go`
- `internal/core/memory/store_batch_parser.go`
- `internal/core/memory/delete.go`
- `internal/vectorstore/sqlitevec/store.go`
- `internal/vectorstore/qdrant/store.go`
- new `internal/core/memory/indexer.go` or `internal/core/indexer/*`

Stop gates:

- [ ] Forced vector failure test leaves rows in `pending` or `failed`, never silently "stored but searchable".
- [ ] Repair job can bring `pending + failed` back to `0`.
- [ ] After batch ingest, `memories`, `memory_embeddings`, and entity/edge projections have zero orphaned IDs.

### [ ] 3. Move tiering, recency, and eval determinism ahead of ranking work

Why this is third:

- If eval runs mutate ranking state, every later F1 number is noisy.
- If almost everything is semantic, tier-aware retrieval and scoring changes are harder to reason about.
- This is a prerequisite for trustworthy benchmarking, not a polish task.

What to do:

- Narrow `tier=auto` semantics so durable and ephemeral memories are meaningfully separated.
- Stop using search-touch side effects in benchmark and eval flows.
- Freeze the comparison config for paperlite gate runs:
  - answer mode
  - answer model
  - answer temperature
  - extractive threshold
  - top-k
  - answer-top-docs
  - parser provider/model/thresholds
  - query count
  - cache reuse/reset policy
- Require median-of-3 runs for the paperlite gate until a fixed generation seed is wired end to end.

Files to track:

- `internal/core/memory/tier_policy.go`
- `internal/core/memory/search.go`
- `internal/core/memory/store.go`
- `internal/api/dto/memory.go`
- `internal/config/config.go`
- `scripts/retrieval_quality.sh`
- `research/eval_locomo_f1_bleu.py`
- `research/run_locomo_paper_aligned_lite.sh`

Stop gates:

- [ ] Paperlite gate runs use `answer_temperature=0.0`.
- [ ] Paperlite gate is reported as median of 3 identical runs.
- [ ] Paperlite gate max spread is `<= 1.5` paper-scale F1 before promotion decisions are made.
- [ ] Repeated eval on the same stored corpus is deterministic when touch is disabled.
- [ ] Tier distribution is plausible and not overwhelmingly semantic by default.

### [ ] 4. Replace the current reranker with a real feature-based hybrid ranker

Why this is fourth:

- RRF candidate selection is fine as a first pass.
- The current reranking discards too much information and then overweights generic recency/importance.

What to do:

- Carry explicit candidate features through ranking:
  - dense similarity
  - dense rank
  - lexical score
  - lexical rank
  - exact entity/slot hit
  - route fit
  - freshness
  - importance
- Apply kind/tier/namespace filters before or during candidate generation.
- Add adaptive candidate expansion for low-confidence queries.
- Return debug ranking factors for inspection.

Implementation progress (2026-03-07):

- [x] Carry explicit candidate features through ranking (dense score/rank, lexical score/rank, RRF, entity-slot signal, route fit, freshness, importance).
- [x] Apply kind/tier filters before and during candidate generation paths (repo filter-aware search + dense metadata filtering).
- [x] Return debug ranking factors for inspection via `POST /v1/memory/search` with `debug=true`.

Files to track:

- `internal/core/memory/search.go`
- `internal/core/memory/query_routing.go`
- `internal/repository/sqlite/memory.go`
- `internal/core/retrieval/ranker.go`
- `internal/core/retrieval/retriever.go`
- `internal/api/dto/memory.go`

Stop gates:

- [ ] Curated retrieval eval reaches Top1HitRate `>= 0.50`.
- [ ] Curated retrieval eval reaches MRR `>= 0.65`.
- [ ] Paperlite mini benchmark reaches Recall@60 `>= 0.45`.
- [ ] Paperlite mini benchmark reaches nDCG@60 `>= 0.16`.
- [ ] Paperlite mini benchmark reaches MRR `>= 0.10`.

### [ ] 5. Replace regex routing with a real query planner

Why this is fifth:

- Routing currently nudges scores; it does not really decide how to answer a query.
- Pali needs typed retrieval paths for direct attributes, temporal facts, aggregations, and multi-hop joins.

What to do:

- Add a planner that outputs:
  - `intent`
  - `confidence`
  - extracted entities
  - extracted relations
  - time constraints
  - required evidence shape
- Route into one of:
  - direct fact lookup
  - temporal lookup
  - aggregation lookup
  - graph/entity expansion
  - hybrid vector fallback
- Make `debug=true` show route, planner output, and fallback chain.

Implementation progress (2026-03-07):

- [x] Add a typed planner output (`intent`, `confidence`, `entities`, `relations`, `time_constraints`, `required_evidence`, `fallback_path`).
- [x] Route into direct/temporal/aggregation/graph-expansion/hybrid fallback intents from one planner entrypoint.
- [x] Expose planner and fallback chain through API debug payload (`debug=true`).

Files to track:

- `internal/core/memory/query_routing.go`
- `internal/core/memory/search.go`
- `internal/core/memory/entity_facts.go`
- `internal/api/dto/memory.go`
- `internal/api/handlers/memory.go`
- `internal/mcp/tools/registry.go`

Stop gates:

- [ ] Temporal route keeps or improves current temporal score.
- [ ] Single-hop and open-domain categories improve without temporal regression.
- [ ] `min_score` works correctly on all paths, including entity-fact retrieval.
- [ ] Debug output explains route and evidence for at least `95%` of benchmark queries.

### [ ] 6. Turn SQLite into an honest local baseline

Why this is sixth:

- Right now sqlite is being asked to prove too much while using a deliberately weak JSON-scan implementation.
- Before comparing Pali to graph-heavy systems, Pali needs a real local baseline, not a misleading one.

What to do:

- Stop treating the current JSON scan as "sqlite vec" in product discussions.
- Either:
  - implement a real sqlite vector extension path, or
  - make the local baseline explicitly optimized around binary vectors, cached norms, and bounded search.
- Separate metadata DB concerns from vector-search implementation concerns.
- Add real batch upsert support to Qdrant so production backend comparisons are fair.

Files to track:

- `internal/vectorstore/sqlitevec/store.go`
- `internal/repository/sqlite/migrations.go`
- `internal/repository/sqlite/db.go`
- `internal/vectorstore/qdrant/store.go`
- `internal/vectorstore/qdrant/client.go`
- `internal/domain/vectorstore.go`
- `internal/wiring/components.go`

Stop gates:

- [x] Backend benchmark scripts support `sqlite` and `qdrant` with the same workload.
- [x] `internal/vectorstore/qdrant/store.go` implements `UpsertBatch`.
- [ ] 3 repeated runs on the same machine stay within `10%` variance for search p95.
- [ ] Any local vector-storage rewrite preserves or improves curated retrieval quality.

### [ ] 7. Add a real memory lifecycle: update, forget, supersede, history

Why this matters:

- A memory product cannot just store/search/delete. It needs correction and audit.
- The current delete-for-replacement flow hides stale-memory failures instead of modeling them.

What to do:

- Add APIs and MCP tools for:
  - `memory_update`
  - `memory_forget`
  - `memory_history`
  - `memory_pin`
  - `memory_reindex`
- Add version/event tables and mark old facts superseded instead of deleting them.

Files to track:

- `internal/api/router.go`
- `internal/api/handlers/memory.go`
- `internal/api/dto/memory.go`
- `internal/mcp/tools/registry.go`
- `internal/domain/memory.go`
- `internal/domain/repository.go`
- `internal/repository/sqlite/migrations.go`
- `internal/repository/sqlite/memory.go`

Stop gates:

- [ ] Updating a fact pushes the old fact out of top-3 within one reindex cycle.
- [ ] Every mutation has an auditable history row.
- [ ] Parser dedupe/update stops hard-deleting memories during normal correction flow.

### [ ] 8. Make the benchmark stack harder to game and easier to trust

Why this matters:

- The current default harness can make progress look bigger than it is.
- The product needs one official scorecard that blocks self-deception.

What to do:

- Keep three benchmark tiers:
  - smoke: tiny local fixture
  - curated retrieval: labeled retrieval regression set
  - paperlite mini: product gate
- Remove first-3-word auto-querying from the default performance benchmark.
- Add repeated-run medians.
- Record wrong-top1 counts, duplicate-top1 share, stale-memory hits, and contradiction hits.
- Treat `paperlite mini` as the score that decides whether the core is good enough.

Files to track:

- `scripts/benchmark.sh`
- `scripts/retrieval_quality.sh`
- `scripts/retrieval_trend.sh`
- `BENCHMARKS.MD`
- `research/eval_locomo_f1_bleu.py`
- `research/README.md`

Stop gates:

- [x] `scripts/benchmark.sh` no longer uses first-3-word queries as its primary search workload.
- [x] `scripts/retrieval_quality.sh` supports non-sqlite backends.
- [ ] Every retrieval/store change updates the curated scorecard and the paperlite mini scorecard.
- [ ] Backend/model changes are blocked until paperlite mini overall F1 is `>= 20.0`.

### [ ] 9. Add explainability and operational visibility

Why this matters:

- Right now Pali can return a wrong memory and give very little evidence about why.
- Debuggability is part of product quality for memory systems.

What to do:

- Add search debug payloads:
  - route chosen
  - candidate ranks
  - dense/lexical features
  - filter decisions
  - indexing state
- Add dashboard views for:
  - indexing backlog
  - stale/superseded memory counts
  - high-duplicate tenants
  - per-kind/tier distributions

Files to track:

- `internal/api/dto/memory.go`
- `internal/api/handlers/memory.go`
- `internal/core/memory/search.go`
- `internal/dashboard/handlers.go`
- `internal/dashboard/templates/*`

Stop gates:

- [ ] For any bad retrieval, the API can answer "why did this rank first?" without reading logs.
- [ ] Benchmark traces include planner, candidate, and answer-path diagnostics by default.

### [ ] 10. Only after all of the above, spend real time on better models/backends

Why this is last:

- The plugin architecture is a strength.
- But at the current stage, plugins mostly amplify core design weaknesses.

What to do after the earlier gates pass:

- Add Qdrant as a truly first-class production backend.
- Evaluate better embedders and rerankers against the locked scorecard.
- Explore typed graph expansion, learned sparse retrieval, and domain tuning.

Files to track:

- `internal/vectorstore/qdrant/*`
- `internal/domain/vectorstore.go`
- `internal/core/memory/search.go`
- `internal/wiring/components.go`
- `research/*`

Stop gates:

- [ ] New backend or model must beat the locked baseline by at least `10%` on paperlite mini overall F1.
- [ ] New backend or model must not regress curated Top1HitRate by more than `2%`.

## Official Benchmark Ladder

This is the benchmark stack I would use going forward.

### Gate A: Smoke regression only

Purpose:

- fast CI sanity
- API correctness
- obvious performance regressions

Inputs:

- `test/fixtures/memories.json`
- `test/fixtures/retrieval_eval.curated.json`

Pass rules:

- [ ] API/unit tests pass.
- [ ] Curated retrieval Top1HitRate does not regress by more than `2%`.
- [ ] Curated retrieval Recall@10 stays `>= 0.85`.

Note:

- This gate is not enough for product claims. It is only for regression protection.

### Gate B: Retrieval quality gate

Purpose:

- measure whether retrieval is getting more useful, not just faster

Pass rules:

- [ ] Top1HitRate `>= 0.50`
- [ ] Recall@10 `>= 0.90`
- [ ] nDCG@10 `>= 0.70`
- [ ] MRR `>= 0.65`

### Gate C1: Core-memory gate

Purpose:

- isolate memory representation and retrieval improvements from answer-model variance

Benchmark:

- `research/data/locomo10.paperlite.mini5.fixture.json`
- `research/data/locomo10.paperlite.mini5.eval.json`
- same backend
- same embedder
- extractive-only answer mode
- touch disabled for eval

Pass rules:

- [ ] Extractive-only overall F1 improves materially from the locked baseline.
- [ ] Recall@60 `>= 0.45`
- [ ] nDCG@60 `>= 0.16`
- [ ] MRR `>= 0.10`
- [ ] Single-hop F1 `>= 10.0`
- [ ] Multi-hop F1 `>= 8.0`
- [ ] Open-domain F1 `>= 7.0`
- [ ] Temporal F1 `>= 35.0`

### Gate C2: Product gate

Purpose:

- decide whether Pali's full product behavior is good enough before plugin chasing

Benchmark:

- `research/data/locomo10.paperlite.mini5.fixture.json`
- `research/data/locomo10.paperlite.mini5.eval.json`
- same backend
- same embedder family
- same answer mode across runs
- same answer model across runs
- `answer_temperature=0.0`
- same parser mode/model/thresholds
- same `top_k`
- same `answer_top_docs`
- same extractive threshold
- same `max_queries`
- same cache reuse/reset policy
- report median of 3 identical runs

Current reference:

- `research/results/mini_runs/reuse_matrix_20260306T133450Z/ollama_hybrid_qwen7b.summary.txt`
- overall F1 `15.17`

Pass rules:

- [ ] Overall F1 `>= 20.0`
- [ ] Recall@60 `>= 0.45`
- [ ] nDCG@60 `>= 0.16`
- [ ] MRR `>= 0.10`
- [ ] Single-hop F1 `>= 10.0`
- [ ] Multi-hop F1 `>= 8.0`
- [ ] Open-domain F1 `>= 7.0`
- [ ] Temporal F1 `>= 35.0`
- [ ] `top1_most_common_share <= 0.10`
- [ ] Median-of-3 spread stays within `1.5` paper-scale F1

If Gate C2 is not passing, Pali should not call the next change a backend/model win.

## Run Commands For The Gates

Use the repo's existing commands so the stop gates stay executable.

### Smoke and curated retrieval

```bash
go test ./internal/... ./pkg/...

scripts/retrieval_quality.sh \
  --fixture test/fixtures/memories.json \
  --eval-set test/fixtures/retrieval_eval.curated.json \
  --top-k 10 \
  --max-queries 0 \
  --embedding-provider ollama \
  --embedding-model mxbai-embed-large
```

### Product gate: paperlite mini benchmark

```bash
research/run_locomo_paper_aligned_lite.sh \
  --num-convs 10 \
  --top-k 60 \
  --max-queries 120 \
  --answer-mode hybrid \
  --answer-model qwen2.5:7b \
  --parser-provider ollama \
  --parser-model qwen2.5:7b \
  --embed-model all-minilm \
  --reuse-cache
```

### Locked run profiles (Windows + Ollama)

Use these as the only comparison profiles for iteration. Do not change them between runs unless intentionally creating a new baseline.

- [ ] Main verification run (full): `--num-convs 10 --max-queries 120`, provider `ollama`, answer mode `hybrid`, parser enabled. Target runtime: ~5 minutes on the same Windows machine.
- [ ] Lite iteration run (fast): lexical-only answer path with `--num-convs 10 --max-queries 120`, same fixture/eval pair and same retrieval flags, used only for quick regression checks.
- [ ] Promotion decisions must use the main verification run profile; lite runs are directional only.

Operational rules:

- [ ] Run the paperlite gate on the same machine when comparing deltas.
- [ ] Lock backend, embedder, parser mode, parser thresholds, answer mode, answer model, extractive threshold, top-k, answer-top-docs, query count, and cache reuse/reset policy before comparing runs.
- [ ] Keep `answer_temperature=0.0`; if the wrapper does not expose it, confirm the evaluator default and treat adding an explicit flag as benchmark debt.
- [ ] Use median of 3 identical paperlite runs for promotion decisions until a fixed seed is wired end to end.
- [ ] Treat `paperlite mini` as the promotion gate and the toy fixture as regression protection only.

## File Tracking Matrix

Use this as the "where do we touch code first?" map.

| File | What to track | First change |
|---|---|---|
| `internal/core/memory/store.go` | non-atomic write path, raw/parsed duplication, delete-for-replacement | split archival/raw write from canonical unit write; stop hard delete on update |
| `internal/core/memory/store_batch_parser.go` | parser batch semantics, lexical dedupe, pending writes | replace lexical pending dedupe with canonical upsert plan |
| `internal/core/memory/search.go` | weak hybrid rerank, fixed candidate window, late filtering | add feature-based ranker, adaptive window, pre-filtering, debug output |
| `internal/core/memory/query_routing.go` | regex routing only | replace with planner/slot extraction |
| `internal/core/memory/entity_facts.go` | narrow relation extraction | add typed relations, provenance, graph edges |
| `internal/core/memory/tier_policy.go` | over-broad semantic promotion | tighten `tier=auto` policy |
| `internal/repository/sqlite/migrations.go` | schema too thin | add versions, events, outbox, indexing state, namespaces/turn IDs |
| `internal/repository/sqlite/memory.go` | missing lifecycle ops, filter-aware queries | add update/history/supersede/reindex queries |
| `internal/repository/sqlite/db.go` | single connection limits throughput and read concurrency | make concurrency strategy explicit and benchmarked |
| `internal/vectorstore/sqlitevec/store.go` | JSON scan masquerading as vector store | implement honest local vector baseline |
| `internal/vectorstore/qdrant/store.go` | missing batch support | implement `UpsertBatch` |
| `internal/domain/memory.go` | memory object too thin | add canonical/lifecycle/indexing metadata |
| `internal/domain/vectorstore.go` | vector API too thin for richer ranking/backfill | extend interfaces for batch and richer candidate features |
| `internal/api/router.go` | lifecycle routes missing | add update/forget/history/reindex endpoints |
| `internal/api/handlers/memory.go` | debug + lifecycle handling missing | support new lifecycle and debug responses |
| `internal/api/dto/memory.go` | response surface too thin | add debug, lifecycle, planner, indexing-state fields |
| `internal/mcp/tools/registry.go` | MCP surface mirrors API gaps | add lifecycle/debug tools |
| `scripts/benchmark.sh` | weak search workload, sqlite-only | remove prefix queries, add backend matrix |
| `scripts/retrieval_quality.sh` | sqlite-only eval | support multiple backends with same labeled set |
| `BENCHMARKS.MD` | benchmark goals not strict enough | document official gates and promotion policy |
| `research/eval_locomo_f1_bleu.py` | product-quality gate runner | standardize locked mini benchmark profile |
| `research/run_locomo_paper_aligned_lite.sh` | wrapper hides some gate knobs | plumb explicit deterministic gate flags |

## What Not To Spend More Time On Right Now

- [ ] More score multipliers in `query_routing.go`.
- [ ] More benchmark-only query variants in research scripts without matching product changes.
- [ ] More dual-write variations for observations/events.
- [ ] New backend/model bake-offs before Gate C passes.

## Recommended Execution Order

- [ ] PR 1: benchmark hardening plus write-path state model
- [ ] PR 2: schema expansion for canonical memories, versions, outbox, indexing state
- [ ] PR 3: canonical parser/store path replacing legacy dual-write behavior
- [ ] PR 4: tiering, touch-control, and eval determinism cleanup
- [x] PR 5: hybrid fusion fix to carry real lexical score features through reranking (not only RRF-derived lexical signal).
- [x] PR 6: apply kind/tier filters before candidate generation and fix `min_score` behavior on entity-fact routes.
- [ ] PR 7: run step-2 transactional indexing hardening in parallel with retrieval work (do not block retrieval fixes on full async indexing).
- [ ] PR 8: add one multi-hop-specific retrieval path (IRCoT-style iterative retrieval) before additional model/backend churn.
- [ ] PR 9: lifecycle APIs and MCP tools
- [ ] PR 10: SQLite baseline cleanup and Qdrant batch parity

If this order is followed, Pali improves where it is actually weak today:

- memory representation
- write correctness
- retrieval correctness
- benchmark trustworthiness

That is the sequence most likely to move the paperlite score from the current mid-teens into the 20s without hiding behind a better backend or a better model.
