# TODO Benchmarks: DeepSeek Precision Recovery (LOCOMO)

Make sure we turn off any old pali servers pid before we run new ones!

and at the end of the document add a runs table where we log runs and see if we do ever get some changes(bold things that need attention) or update the run comparion table below every run!

## Objective
Raise non-temporal top-1 factual precision (Multi-hop/Open-domain/Single-hop) while preserving temporal gains, using a strict test-and-run feedback loop.

## Evidence Snapshot (from latest analysis)

### Run comparison table
| Run | Recall@60 | F1 Overall | Temporal F1 | Multi-hop F1 |
|---|---:|---:|---:|---:|
| Baseline extractive (no routing, no structured) | 42.9% | 4.88 | 9.28 | 1.11 |
| Structured extractive (routing ON) | 5.67% | 5.66 | 13.72 | 0.71 |
| Cached hybrid 10q (no structured, deepseek) | 40.0% | 17.33 | 28.89 | 0.00 |
| Structured hybrid 120q (deepseek) | 1.25% | 10.46 | 26.25 | 3.79 |
| Structured hybrid 120q (phi4-mini) | 1.25% | 12.22 | 26.25 | 4.34 |
| Latest parser benchmark (phi4-mini) | 38.6% | 12.26 | 26.85 | 4.34 |
| M1 run (deepseek, 2026-03-05 22:50 UTC) | 38.26% | 10.83 | 26.85 | 3.79 |
| M1+M2+M3+P2 run (deepseek, 2026-03-06 00:36 UTC) | 38.96% | 10.89 | 26.85 | 3.79 |

### What this means
- Temporal extraction logic is useful, but retrieval correctness is unstable.
- Structured + kind routing can collapse recall in some profiles.
- Multi-hop remains bottlenecked by retrieval and memory representation, not generator style.

## Confirmed Root Causes to Address First

### Bug 1: Kind routing can make answers unreachable
- Temporal queries are routed to `event/observation`, but temporal anchor often only lives in `raw_turn`.
- If `raw_turn` is excluded, question is impossible even with large `top_k`.

### Bug 2: Heuristic parser strips temporal anchors
- Fact extraction normalizes to terse facts like `Caroline attended LGBTQ support group`.
- Timestamp context from source turn is lost in stored fact content.
- Extractive answer then picks wrong date from wrong retrieved memory.

### Multi-hop failure mode
- Query semantics and stored fact wording drift apart (`research` vs `adoption agency interviews`).
- Single-pass retrieval returns generic recent snippets; oracle often contains nearby but not top-ranked evidence.

## Priority Architecture Changes (P1)

### 1) Timestamp injection at parse/store time
- Change: when parser derives a fact from annotated turn, inject normalized time anchor into fact content.
- Goal: date exists in both embedding text and extractable text.
- Expected gain: `+8 to +12` F1 (mostly Temporal).

### 2) Always include `raw_turn` for temporal routes
- Change: temporal routing must append `raw_turn` as mandatory fallback route.
- Goal: prevent recall collapse when anchors exist only in raw turn.
- Expected gain: `+6 to +10` F1 via recall recovery.

### 3) Two-pass retrieval for low-confidence multi-hop
- Change:
  - pass1 retrieve
  - if low confidence, extract anchor entity/snippet from top evidence
  - pass2 query with anchor + original keywords
  - merge by RRF
- Goal: composition over single-shot retrieval.
- Expected gain: `+5 to +8` F1 on Multi-hop.

### 4) Upgrade embedding model for retrieval robustness
- Change: evaluate `nomic-embed-text` and `mxbai-embed-large` against `all-minilm` under identical setup.
- Goal: better semantic alignment for conversational paraphrase.
- Expected gain: `+3 to +6` F1 across categories.

### 5) Minimum fact-length floor in parser store path
- Change: enforce minimum content length (e.g., `>= 25 chars`) before persisting parsed facts.
- Goal: reduce high-recall low-value short facts.
- Expected gain: `+1 to +2` F1 precision lift.

## Implementation Milestones (tests + accountable runs)

### M0: Freeze baseline + instrumentation
- [ ] Baseline run with deepseek profile (120 queries).
- [x] Capture `store_diagnostics.mode`, `batch_size`, `batch_fallbacks`.
- [ ] Record category miss-rate and hit-but-wrong counts from trace.

Deliverables:
- [ ] `comparison_vs_baseline.json` in run dir.
- [ ] Updated table in this doc.

### M1: Timestamp injection
Files:
- [x] `internal/core/memory/structured_observations.go` — `normalizeTurnTimeAnchor` now returns "D Mon YYYY" (e.g., "8 May 2023")
- [x] `internal/core/memory/store.go` — `prepareParsedFactsForStore` now prepends "On {date}, " instead of tag suffix
- [x] parser tests — updated expectations to match natural-language format

Tests:
- [x] Parsed fact includes expected normalized date when source turn has time.
- [x] Existing non-temporal fact tests still pass.
- [x] Stored content format: "On 8 May 2023, Caroline attended LGBTQ support group."

Run gate:
- [x] **M1 bug was fixed:** Prior run (20260305T225007Z) failed because ISO format wasn't extractable. Now fixed to human-readable.
- [x] **Debug test outcome (M1+M2 combined):** Temporal F1 66.67%, oracle rank 2. Both M1+M2 together working.
- [ ] Isolated M1+M2 validation on full 120q run (needed to assess individual impact).
- [ ] Temporal F1 expected to improve from baseline 26.85% due to:
  - M1: date anchors now embedded in fact text, more embedding-relevant
  - M2: raw_turn included in temporal routing, oracle memories reachable

### M2: Temporal raw_turn fallback routing
Files:
- [x] `research/eval_locomo_f1_bleu.py` — updated `build_retrieval_routes()` to include raw_turn in temporal route
- [x] `internal/core/memory/search.go` (no changes needed — already applies boost to raw_turn)
- [x] `internal/core/memory/query_routing.go` (no changes needed)

Tests:
- [x] Temporal query returns `raw_turn` candidates with routing enabled.
- [x] Non-temporal routing behavior unchanged (person/multihop routes unaffected).

Run gate:
- [x] **Single-query debug test (locomo_conv-26, 8 May 2023 LGBTQ support group q):**
  - Before M2: hit_rank 46, extractive_answer "20 July 2023" (wrong), F1=33.33%
  - After M2: hit_rank 2, extractive_answer "8 May 2023" (±1 day), F1=66.67%
  - **2x F1 improvement, oracle moved rank 46→2**
- [ ] Full 120q run needed to confirm category-level gains and measure regressions.

### M3: Two-pass retrieval for low-confidence multi-hop
Files:
- [x] `research/eval_locomo_f1_bleu.py` — two-pass retrieval logic for multi-hop queries:
  - `extract_anchor_from_top_results()` — extract entity/phrase from pass1 top-3
  - `build_two_pass_query()` — combine original query with anchor
  - Main loop: trigger pass2 when multi-hop detected, merge via RRF
  - Trace output: `two_pass_performed`, `two_pass_anchor` fields

Tests:
- [x] Code syntax verified, ready for benchmark run
- [ ] Multi-hop synthetic fixture where pass1 misses but pass2 recovers expected evidence.
- [ ] No regression for temporal query latency guardrails.

Run gate:
- [ ] Deepseek 120q run (M1+M2+M3 combined).
- [ ] Multi-hop F1 and MRR improve vs baseline or M1+M2-only run.
- [ ] Temporal F1 does not regress.

**Note:** M3 is eval-harness-only (benchmark optimization). See ARCHITECTURE_NOTES.md for future refactor into pali core.

### M4: Embedding model bake-off
Files:
- [ ] research harness scripts/config docs

Tests:
- [ ] smoke run for each model with identical flags.

Run gate:
- [ ] Compare at least 2 alternative embedders against `all-minilm`.
- [ ] Promote only if non-temporal categories improve without temporal regression.

### M5: Fact-length precision floor
Files:
- [x] `internal/core/memory/store.go` (`applyParsedFact` stage)
- [x] parser/store tests

Tests:
- [x] short low-info facts rejected.
- [x] high-signal short facts from allowlist still accepted.

Run gate:
- [ ] Deepseek 120q run.
- [ ] Top1 repetition share decreases; non-temporal F1 improves.

## DeepSeek-Only Standard Profile (default for future LOCOMO)

### Model + parser defaults
- Answer model: `deepseek-r1:7b`
- Parser: `enabled=true`, `provider=heuristic`, `store_raw_turn=true`
- Parser thresholds: `max_facts=5`, `dedupe=0.88`, `update=0.94`
- Retrieval: `top_k=60`, `kind_routing=true`, `prefer_extractive_for_temporal=true`

### Store path requirement
- Must use batch path when available (`/v1/memory/batch`) with Phase-5 StoreBatch internals.
- Required run summary fields:
  - `Store mode`
  - `Store batch size`
  - `Store fallbacks`

### Config/flag sanity checklist before each run
- [ ] `answer_model` is `deepseek-r1:7b`.
- [ ] Parser block is present in generated eval YAML.
- [ ] Structured dual-write flags remain OFF when parser is enabled.
- [ ] `store_diagnostics.mode` is `batch` or `reuse_existing_store` (not accidental single).
- [ ] No reuse after interrupted `--reset-db` run.

### Debug-First Verification Policy
- For debug/verification, run a small targeted slice first to confirm the expected behavior before paying ingest cost.
- Use one of:
  - single-tenant fixture/eval subset (for exact query behavior checks)
  - `--max-queries 10..30` with `--reuse-existing-store` (for quick metric direction)
- If the targeted check matches expectations, run the full DeepSeek 120q benchmark only for milestone/major changes (M1/M2/M3/M4/M5 gates).
- If store-path logic changed (parser/store/routing affecting stored text), do one clean rebuild run with `--reset-db`, then iterate fast with `--reuse-existing-store`.

## Accountable Feedback Template (fill every milestone)
- [ ] Run ID:
- [ ] Commit hash:
- [ ] Scenario (M0/M1/M2/M3/M4/M5):
- [ ] Store diagnostics (`mode`, `batch_size`, `fallbacks`):
- [ ] Overall (`F1`, `BLEU-1`, `Recall@60`, `MRR`, `nDCG@60`):
- [ ] Category F1 (`Multi-hop`, `Open-domain`, `Single-hop`, `Temporal`):
- [ ] Miss-rate (`M/O/S`):
- [ ] Hit-but-wrong count (`M/O/S`):
- [ ] Top1 most common share:
- [ ] Decision: promote / iterate / rollback

## P2: Entity Store (Aggregation / Global Query Fix)

### Problem
Multi-hop aggregation queries ("what activities does X do?", "list all events Y attended") are structurally unsolvable by vector search. The answer is the union of 3–6 separate memories spread across the corpus. Single-pass top-k retrieval returns generic recent snippets instead.

This is the **GraphRAG problem**: vector search is a local query engine; aggregation requires global traversal.

### Solution: Entity-facts table alongside vector store

At store time, the parser emits structured triples in addition to fact text:

```sql
CREATE TABLE entity_facts (
  id          TEXT PRIMARY KEY,
  tenant_id   TEXT NOT NULL,
  entity      TEXT NOT NULL,   -- "Melanie"
  relation    TEXT NOT NULL,   -- "activity"
  value       TEXT NOT NULL,   -- "camping"
  memory_id   TEXT REFERENCES memories(id),
  created_at  TEXT NOT NULL
);
CREATE INDEX entity_facts_lookup ON entity_facts(tenant_id, entity, relation);
```

At query time, a query classifier detects aggregation intent (keywords: *what all, list, activities, events, things, places, books, hobbies, interests, participated, done*) and issues a structured SELECT instead of (or alongside) vector search:

```sql
SELECT value FROM entity_facts WHERE tenant_id = ? AND entity = ? AND relation = ?
```

### Why this doesn't require Neo4j
- SQLite, same single-binary constraint
- The table IS a graph — entity nodes are rows grouped by `entity`, edges are `relation` columns
- Cross-entity traversal (true multi-hop chains) added later as a JOIN when extraction is reliable enough

### Upgrade path
- **Phase 1 (heuristic)**: parser emits triples from facts it already extracts; no LLM at ingest time
- **Phase 2 (ollama parser)**: opt-in richer triple extraction with `--parser-provider ollama`; enables entity-to-entity edges
- **Phase 3 (graph)**: swap SQLite adjacency table for DuckDB or neo4j via same interface

### Expected impact
- Multi-hop aggregation F1: ~4% → ~25–35%
- No regression on temporal or single-hop (vector path unchanged)

### Files to change
- [x] `internal/domain/repository.go` — add `EntityFactRepository` interface
- [x] `internal/repository/sqlite/migrations.go` — add `entity_facts` table migration
- [x] `internal/repository/sqlite/entity_facts.go` — CRUD + bulk insert
- [x] `internal/core/memory/store_batch_parser.go` — emit triples alongside facts in `applyParsedFactWithPending`
- [x] `internal/core/memory/info_parser_heuristic.go` — extend `ParsedFact` with optional `Entity`, `Relation`, `Value` fields
- [x] `internal/core/memory/search.go` — aggregation query branch hitting entity_facts
- [x] `internal/core/memory/query_routing.go` — aggregation intent detection
- [x] `research/eval_locomo_f1_bleu.py` — new retrieval kind `entity` for aggregation questions in cat1

### Run gate
- [ ] Cat1 (Multi-hop) F1 improves vs M3 milestone
- [x] Cat2/Cat4 F1 does not regress
- [ ] Aggregation synthetic fixture: all expected values appear in answer
- Latest clean 120q deepseek run (`20260306T001938Z_m1m2m3p2_deepseek120`) summary:
  - Cat1 (Multi-hop) F1: `3.789` (unchanged vs `20260305T225007Z`)
  - Cat2 (Temporal) F1: `26.847` (unchanged)
  - Cat4 (Single-hop) F1: `4.324` (improved from `3.892`)
  - Open-domain F1 regressed: `2.702` -> `1.653` (needs follow-up)

---

## Current Baseline Artifacts to Compare Against
- Deepseek 120q clean run:
  - `research/results/p1_parser_benchmark/20260305T211801Z/ollama.json`
  - `research/results/p1_parser_benchmark/20260305T211801Z/ollama.summary.txt`
  - `research/results/p1_parser_benchmark/20260305T211801Z/ollama.trace.jsonl`
- Cross-run comparison:
  - `research/results/p1_parser_benchmark/20260305T211801Z/comparison_vs_baseline_and_phi.json`

## Latest Run Log (M1)
- Run ID: `20260305T225007Z`
- Commit hash: `e65da3c`
- Scenario: `M1`
- Store diagnostics (`mode`, `batch_size`, `fallbacks`): `batch`, `64`, `0`
- Overall (`F1`, `BLEU-1`, `Recall@60`, `MRR`, `nDCG@60`): `0.108342`, `0.079063`, `0.382639`, `0.042188`, `0.105874`
- Category F1 (`Multi-hop`, `Open-domain`, `Single-hop`, `Temporal`): `0.037890`, `0.027018`, `0.038923`, `0.268468`
- Top1 most common share: `0.191667`
- **Note:** M1 was broken in this run (ISO timestamp format, not extractable). Fixed 2026-03-05 22:58 UTC.

## Latest Run Log (M1+M2+M3+P2)
- Run ID: `20260306T001938Z_m1m2m3p2_deepseek120`
- Commit hash: `e65da3c`
- Scenario: `M1+M2+M3+P2 (clean deepseek 120q)`
- Store diagnostics (`mode`, `batch_size`, `fallbacks`): `batch`, `64`, `0`
- Migration sanity: `entity_facts` table + lookup/dedupe indexes present; run stored `9344` entity_facts rows.
- Overall (`F1`, `BLEU-1`, `Recall@60`, `MRR`, `nDCG@60`): `0.108856`, `0.079417`, `0.389583`, `0.044114`, `0.109069`
- Category F1 (`Multi-hop`, `Open-domain`, `Single-hop`, `Temporal`): `0.037890`, `0.016529`, `0.043243`, `0.268468`
- Top1 most common share: `0.191667`
- Comparison vs last iteration (`20260305T225007Z`):
  - Recall@60: `+0.006944` (`38.26%` -> `38.96%`)
  - F1 overall: `+0.000514` (`10.83` -> `10.89`)
  - Multi-hop F1: `0.000000` (no change)
  - Temporal F1: `0.000000` (no change)
  - Single-hop F1: `+0.004320`
  - Open-domain F1: `-0.010490`
- Artifacts:
  - `research/results/p1_parser_benchmark/20260306T001938Z_m1m2m3p2_deepseek120/ollama.json`
  - `research/results/p1_parser_benchmark/20260306T001938Z_m1m2m3p2_deepseek120/ollama.summary.txt`
  - `research/results/p1_parser_benchmark/20260306T001938Z_m1m2m3p2_deepseek120/ollama.trace.jsonl`
  - `research/results/p1_parser_benchmark/20260306T001938Z_m1m2m3p2_deepseek120/comparison_vs_20260305T225007Z.json`
- Decision: `iterate` (**Cat1 unchanged + Open-domain regression**)

## M1+M2 Combined Validation (single-query debug test)
- Query: "When did Caroline go to the LGBTQ support group?"
- Reference: "7 May 2023"
- Tenant: locomo_conv-26 (419 rows, subset of LOCOMO)
- Before M1+M2 fix:
  - Oracle hit_rank: 46
  - Extractive answer: "20 July 2023" (wrong event)
  - F1: 0.333333 (33.33%, partial token match)
- After M1+M2 fix (2026-03-05 23:05 UTC):
  - Oracle hit_rank: 2
  - Extractive answer: "8 May 2023" (±1 day error)
  - F1: 0.666667 (66.67%, major improvement)
- **Result:** M1+M2 combined shows 2x F1 improvement on this temporal query. Next: full 120q run to measure category-level impact.
- Temporal wrong-year mismatch (heuristic, 33 rows with years): `4/33` (unchanged vs `20260305T211801Z`)
- Comparison artifact: `research/results/p1_parser_benchmark/20260305T225007Z/comparison_vs_20260305T211801Z.json`
- Decision: `iterate` (temporal stable; recall/MRR up; open-domain dipped)

## Runs Table (rolling)
| Run ID | Scenario | F1 Overall | Recall@60 | Multi-hop F1 | Temporal F1 | Decision |
|---|---|---:|---:|---:|---:|---|
| `20260305T225007Z` | M1 (deepseek 120q) | 10.83 | 38.26% | 3.79 | 26.85 | iterate |
| `20260306T001938Z_m1m2m3p2_deepseek120` | M1+M2+M3+P2 (deepseek 120q, clean) | 10.89 | 38.96% | 3.79 | 26.85 | iterate (**Cat1 unchanged, Open-domain down**) |
