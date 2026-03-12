# Multi-Hop Graph Upgrade Plan (Graphiti-Inspired) for Pali

Status: draft for implementation review  
Owner: memory/retrieval maintainers  
Last updated: 2026-03-11

## 1. Objective

Improve multi-hop retrieval quality in Pali by evolving the current entity-fact bridge into a path-aware graph retrieval system, while preserving code quality, backwards compatibility, and benchmark rigor.

This plan defines:
- what to change in code and where
- config additions and rollout strategy (including default-on criteria)
- LOCOMO harness updates
- a 200-query evaluation suite that includes LOCOMO and non-LOCOMO tracks

## 2. Current Problems to Solve

Observed in current codebase:
- Multi-hop relies heavily on lexical expansion and shallow graph lookup.
- Neo4j graph fetch is mostly recency-sorted fact retrieval, not path-scored traversal.
- Entity seed and bridge candidate caps are small for hard multi-hop questions.
- Entity facts are not fully temporal/validity-aware (no invalidation lifecycle).

Key files involved:
- `internal/core/memory/search.go`
- `internal/core/memory/query_routing.go`
- `internal/repository/neo4j/entity_facts.go`
- `internal/domain/memory.go`
- `internal/domain/repository.go`

## 3. Design Principles (Code Quality First)

1. Keep interfaces explicit and small.
- Add new optional interfaces in `internal/domain/repository.go`.
- Avoid overloading existing methods with hidden behavior.

2. Preserve backward compatibility.
- New config keys must be optional with safe defaults.
- Existing retrieval path must continue to work unchanged when feature flags are off.

3. Add test coverage with each behavior change.
- Unit tests for parsing/planning/ranking.
- Integration tests for Neo4j path retrieval.
- Eval regression checks before enabling by default.

4. Prefer deterministic retrieval logic over opaque prompts.
- LLM decomposition can assist query expansion, but path retrieval and ranking logic should be deterministic and testable.

5. Stage rollout with clear kill switches.
- Feature flags for graph path retrieval.
- One-flag fallback to current behavior.

## 4. Target Architecture

### 4.1 New retrieval capability

Introduce path-aware retrieval over entity graph:
- seed entities from planner + top lexical evidence
- traverse graph paths up to N hops
- score candidates with graph-specific signals:
  - path length penalty
  - support count / path redundancy
  - temporal validity (when enabled)
  - lexical alignment with user query

### 4.2 Temporal fact lifecycle

Extend `EntityFact` model with temporal validity semantics:
- `observed_at`
- `valid_from`
- `valid_to` (nullable / open interval)
- `invalidated_by_fact_id` (optional)
- `confidence`

For singleton relations (ex: `identity`, `role`, `place`), new contradictory facts should close older active facts.

Migration execution rule for this work:
- write the migration/schema code during implementation, but do not run migrations directly against active benchmark/dev stores while work is still in flight
- keep migration changes additive and compatibility-safe until end-to-end testing time
- when migration-bearing test runs are actually needed, stop and request explicit user permission before executing them

### 4.3 Episode anchor (optional but recommended)

Add `PaliEpisode` concept (source turn / grouped event) to improve multi-hop evidence linking:
- facts and memories can reference an episode id
- path retrieval can prefer coherent episode neighborhoods

## 5. Implementation Workstreams

## 5.1 Domain model and interfaces

Files:
- `internal/domain/memory.go`
- `internal/domain/repository.go`

Changes:
- Extend `EntityFact` struct with temporal fields listed above.
- Add optional graph path interface, e.g.:
  - `EntityFactPathRepository`
  - method like `ListByEntityPaths(...)` returning fact/memory ids plus path metadata.

Acceptance:
- Existing repositories compile unchanged (optional interface pattern).
- New fields are no-ops when not populated.

## 5.2 Neo4j repository: path retrieval + temporal filtering

Files:
- `internal/repository/neo4j/entity_facts.go`
- add tests in `internal/repository/neo4j/entity_facts_test.go`

Changes:
- Add Cypher path traversal query supporting:
  - seed entities
  - max hops
  - limit
  - optional relation hints
  - optional temporal validity filter
- Return path metadata needed for ranking.
- Add write logic for invalidation/closing windows for singleton relations.

Acceptance:
- Deterministic results for fixture graph in tests.
- No cross-tenant leakage.
- Stable behavior for empty/no-path cases.

## 5.3 Search pipeline integration

Files:
- `internal/core/memory/search.go`
- `internal/core/memory/query_routing.go`
- `internal/core/memory/service.go`

Changes:
- Introduce graph-path candidate collection stage (separate from current neighborhood lookup).
- Expand seed/entity limits for graph mode via config.
- Add graph scoring terms in ranking blend.
- Maintain current route as fallback when graph route has low confidence or no results.

Acceptance:
- Existing tests pass.
- New tests prove:
  - graph route improves multi-hop retrieval on controlled fixtures
  - fallback route still works
  - filter semantics (`kinds`, `tiers`, `min_score`) remain correct

## 5.4 Config surface

Files:
- `internal/config/config.go`
- `internal/config/defaults.go`
- `internal/config/validation.go`
- `pali.yaml.example`
- `docs/configuration.md`

Add under `retrieval.multi_hop`:
- `graph_path_enabled: false`
- `graph_max_hops: 2`
- `graph_seed_limit: 12`
- `graph_path_limit: 128`
- `graph_min_score: 0.12`
- `graph_weight: 0.25`
- `graph_temporal_validity: false`
- `graph_singleton_invalidation: true` (write-path behavior gate)

These must be additive to the Neo4j config flags that already exist in the repo:
- `entity_fact_backend=neo4j`
- `neo4j.uri`
- `neo4j.username`
- `neo4j.password`
- `neo4j.database`
- `neo4j.timeout_ms`
- `neo4j.batch_size`

Compatibility rule:
- do not silently reinterpret existing Neo4j config keys
- keep graph-path behavior controlled only by the new `retrieval.multi_hop.*` flags
- this preserves current Neo4j-backed benchmark behavior until graph mode is explicitly enabled

Validation rules:
- `graph_max_hops >= 1 && <= 4`
- limits > 0
- weights in `[0,1]`

Default strategy:
- Keep `graph_path_enabled=false` initially.
- Enable by default only after passing criteria in Section 9.
- Do not change the global default `entity_fact_backend` as part of the initial graph rollout.
- If this work graduates successfully, the first default-on promotion should be the graph retrieval flags for Neo4j-backed installs, not a forced backend switch for all users.

Docs update requirement:
- update `docs/configuration.md`
- update `pali.yaml.example`
- document the new graph flags explicitly while rollout is gated
- call out that existing Neo4j settings remain valid and unchanged

## 5.5 Eval harness and benchmark system

Files:
- `research/eval_locomo_f1_bleu.py`
- `research/prepare_locomo_eval.py`
- `scripts/retrieval_quality.sh`
- `scripts/retrieval_trend.sh`
- `BENCHMARKS.MD`

Changes:
1. Add per-category reporting and weighted overall rollup.
2. Add a multi-suite evaluator (LOCOMO + non-LOCOMO) with a fixed 200-query budget.
3. Persist run manifest with:
- config fingerprint
- suite composition
- provider/backend profile
- feature flags used

Output additions:
- `overall_200.summary.json`
- `overall_200.category_breakdown.json`
- trend entry with suite id and graph feature toggles.

## 6. 200-Query Evaluation Suite (LOCOMO + Others)

Target total: ~200 queries per official quality run.

### 6.1 Suite composition

1. LOCOMO core: 120
- 30 single-hop
- 30 temporal
- 40 multi-hop
- 20 open-domain/profile

2. External multi-hop QA adapted to memory retrieval: 40
- 20 HotpotQA-style 2-hop conversions
- 20 MuSiQue-style compositional conversions

3. Pali real-world app traces: 40
- 15 support-agent memory updates/conflicts
- 15 coding-assistant project memory flows
- 10 personal-assistant profile + preference drift

Notes:
- External sets must be converted into Pali eval format (`tenant_id`, `query`, expected ids/indexes).
- For reproducibility, keep fixed sampled IDs and seed.

### 6.2 Why this mix

- LOCOMO keeps continuity with existing research.
- External compositional sets stress graph/path reasoning beyond one benchmark.
- App traces validate infra reality (tenant isolation, evolving facts, stale fact replacement).

## 7. Proposed Metrics and Gates

Primary (overall and per-category):
- Top1Accuracy
- Top5Accuracy
- Recall@5
- MicroRecall@5
- Hits/Relevant
- MRR
- nDCG@5

Graph-specific diagnostics:
- path_supported_hit_rate
- average_hops_of_hit
- temporal_conflict_error_rate
- stale_fact_win_rate (lower is better)

Operational:
- p50/p95/p99 search latency
- ingest throughput
- failure rate

## 8. LOCOMO Harness Changes in Detail

## 8.1 `research/eval_locomo_f1_bleu.py`

Add:
- category-aware slices aligned to suite composition
- graph-route diagnostics extraction from trace artifacts
- weighted combined score for 200-suite runs

## 8.2 `scripts/retrieval_quality.sh`

Add flags:
- `--suite-id <name>`
- `--suite-manifest <path>`
- `--graph-path-enabled true|false`
- `--graph-max-hops <n>`

Emit:
- suite metadata into result JSON
- per-category metrics for LOCOMO and non-LOCOMO subsets

## 8.3 `BENCHMARKS.MD`

Update official gate definition to include:
- `overall_200` gate
- per-category floors (especially multi-hop)
- explicit promotion policy for enabling graph defaults

Benchmark-safety requirement:
- existing benchmark commands and profiles must keep their current behavior unless a new graph flag is passed
- graph-enabled benchmark profiles should be additive, not replacements for existing ones
- result manifests must record graph flags so current and upgraded runs remain directly comparable
- do not rename old profiles in place and blur historical trend lines

## 9. Default-On Rollout Policy

Graph path retrieval can be set ON by default only if all conditions hold for two consecutive weekly runs:

1. Quality:
- overall Top5Accuracy: no regression > 1%
- multi-hop Top5Accuracy: +10% relative minimum improvement
- multi-hop Recall@5: +10% relative minimum improvement

2. Reliability:
- zero tenant leakage incidents in eval/integration tests
- no increase in error rate > 0.5pp

3. Performance:
- p95 search latency regression <= 20%
- no material ingest regression (>10%) on baseline fixture

4. Maintainability:
- all new config options documented
- new behavior covered by tests and changelog entry

When satisfied:
- set `retrieval.multi_hop.graph_path_enabled=true` in `defaults.go`
- mirror in `pali.yaml.example`
- update docs and release notes in same PR.

Neo4j-specific default-on policy:
- if we are satisfied with the results, move the new graph retrieval flags to default-on for installations already using `entity_fact_backend=neo4j`
- do not automatically flip the repository-wide backend default to Neo4j in the same step
- if we later want a Neo4j-first recommended profile, document it explicitly as a profile/recommendation and update docs accordingly

## 10. PR Plan (Recommended)

PR-1: Domain + config schema + validation (no behavior change)  
PR-2: Neo4j path query + tests  
PR-3: Search integration + ranking blend + tests  
PR-4: Temporal invalidation write path + tests  
PR-5: Eval harness 200-suite + benchmark docs  
PR-6: Default-on decision PR (only after gates pass)

Each PR must include:
- tests
- docs diff
- rollback note (how to disable quickly)
- note whether any migration code was added, and confirm that migration execution is deferred until explicit approval for test runs

## 11. Risks and Mitigations

Risk: Overfitting to LOCOMO  
Mitigation: 200-suite includes external and app-trace sets.

Risk: Graph route latency spikes  
Mitigation: hard traversal caps + path limit + fallback path.

Risk: Incorrect temporal invalidation  
Mitigation: relation-scoped invalidation rules + invariants tests.

Risk: Config drift/doc drift  
Mitigation: update `docs/configuration.md` and `pali.yaml.example` in same PR as config changes.

Risk: Ongoing benchmark disruption from schema/config churn  
Mitigation:
- keep all new behavior behind additive flags
- avoid changing active benchmark profiles in place
- defer migration execution until code is ready and explicit approval is given

## 12. Definition of Done

Done means:
1. Graph path retrieval exists and is gated by config.
2. Temporal fact lifecycle works for targeted relations.
3. 200-query suite is runnable and tracked in trends.
4. Multi-hop quality improves meaningfully without harming overall quality.
5. Default-on decision is made from evidence, not assumptions.
