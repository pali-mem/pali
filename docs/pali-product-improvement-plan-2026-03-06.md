# Pali Product Improvement Plan

Date: 2026-03-06
Updated: 2026-03-07 (Top-5 production benchmark redesign)

## Executive Summary

This plan is now production-first.

From this update onward, Pali is not gated by LOCOMO-style answer metrics (`F1`, `BLEU`) for core product decisions. Those remain optional research diagnostics.

Primary product gates now use our retrieval data with `top_k=5` and prioritize:

- `Top1Accuracy` (rank-1 correctness)
- `Top5Accuracy` (any relevant hit in top 5)
- `Recall@5` and `MicroRecall@5`
- `Hits@5` and `Hits/Relevant`

Why this shift:

- Production users care about whether the right memory is retrievable now.
- Answer-generation metrics mix retrieval quality with model variance.
- We need stable, actionable gates that map directly to user-visible recall quality.

## Product Scorecard (New Default)

All official benchmark gates run with `top_k=5`.

| Metric | System Field | Why it matters |
|---|---|---|
| Top1Accuracy | `top1_hit_rate` | correctness of first result |
| Top5Accuracy | `hit_rate_at_k` | chance user gets at least one useful hit |
| Recall@5 | `recall_at_k` | relevant coverage per query |
| MicroRecall@5 | `micro_recall_at_k` | relevant coverage over full run |
| Hits@5 | `total_hits_at_k` | raw count of relevant hits returned |
| Hits/Relevant | `total_hits_at_k / total_relevant` | aggregate retrieval yield |

Secondary metrics (diagnostic only):

- `nDCG@5`
- `MRR`
- latency/throughput from `scripts/benchmark.sh`

## Data and Benchmark Scope

Use this gate order:

1. Curated retrieval set on internal fixture data (`test/fixtures/retrieval_eval.curated.json`)
2. Expanded internal curated sets as we add coverage
3. Auto-prefix eval mode only for smoke checks

What is no longer a promotion gate:

- paperlite overall `F1`
- category `F1`
- `BLEU`

These can still run as research comparisons, but they do not block or approve product promotions.

## Main Risks (Still True)

Independent of metric changes, these are the core product risks:

- non-atomic write/index lifecycle
- weak canonical memory representation
- candidate generation and filtering correctness
- routing quality and explainability gaps
- benchmark drift and non-determinism

The execution order below keeps these as engineering priorities while measuring success with Top-5 retrieval quality.

## Priority Plan

### [ ] 1. Make source-grounded atomic facts the primary retrieval unit

What to do:

- make atomic facts the canonical searchable unit
- preserve source-grounded identity and provenance
- remove duplicate retrieval representations that destabilize ranking

Stop gates:

- [ ] Curated `Top5Accuracy >= 0.75`
- [ ] Curated `Recall@5 >= 0.70`
- [ ] Curated `Hits/Relevant >= 0.70`
- [ ] No regression in `Top1Accuracy`

### [ ] 2. Make writes/deletes transactional from product point of view

What to do:

- add indexing state (`pending/indexed/failed/tombstoned`)
- write metadata and indexing jobs atomically
- add repair/reconcile path

Stop gates:

- [ ] Failure injection never leaves silent index drift
- [ ] Repair can bring `pending+failed` to zero
- [ ] Post-repair retrieval on curated set returns baseline-or-better `Top5Accuracy`

### [ ] 3. Lock eval determinism and tier behavior before more ranking tuning

What to do:

- disable search-touch effects for eval by default
- lock comparison config
- keep tier semantics meaningful

Stop gates:

- [ ] Same corpus + same config gives stable Top-5 metrics across repeated runs
- [ ] run-to-run spread stays within `<= 2%` on `Top5Accuracy`
- [ ] tier distribution remains plausible (not semantic-heavy collapse)

### [ ] 4. Keep feature-based hybrid ranking and tune for Top-5 usefulness

What to do:

- keep dense + lexical + route + freshness + importance features
- enforce pre-filtering and adaptive expansion
- expose ranking debug payload

Stop gates:

- [ ] Curated `Top1Accuracy >= 0.45`
- [ ] Curated `Top5Accuracy >= 0.80`
- [ ] Curated `Recall@5 >= 0.75`
- [ ] Curated `Hits/Relevant >= 0.75`

### [ ] 5. Replace regex-heavy routing with typed planner behavior

What to do:

- planner output: intent, confidence, entities, relations, time constraints
- deterministic fallback chain
- full route/debug visibility

Stop gates:

- [ ] Planner-covered routes maintain or improve `Top5Accuracy`
- [ ] `min_score` behaves correctly on all retrieval paths
- [ ] Route/evidence debug available for >= `95%` of evaluated queries

### [ ] 6. Keep SQLite as an honest local baseline

What to do:

- avoid overstating JSON-scan vector behavior
- keep backend comparisons fair (`sqlite`, `qdrant`)
- keep Qdrant batch parity and repeatability

Stop gates:

- [ ] backend benchmark scripts support same Top-5 workload
- [ ] 3 repeated runs stay within `10%` latency variance
- [ ] retrieval quality does not regress while tuning storage path

### [ ] 7. Add lifecycle APIs (update/forget/history/supersede)

What to do:

- lifecycle endpoints + MCP tools
- version/event history for corrections
- reduce hard-delete replacement behavior

Stop gates:

- [ ] correcting a fact updates Top-5 retrieval within one reindex cycle
- [ ] mutation history is queryable and auditable
- [ ] no hidden stale fact dominance after correction

### [ ] 8. Keep benchmark stack hard to game and easy to trust

What to do:

- enforce a single official Top-5 scorecard
- always publish hit-count metrics (`total_hits_at_k`, `total_relevant`)
- require curated scorecard updates for retrieval changes

Stop gates:

- [ ] every retrieval/store PR posts curated Top-5 scorecard delta
- [ ] scorecards include fixture/eval hashes and commit hash
- [ ] promotion decisions use Top-5 gates, not paper metrics

### [ ] 9. Improve explainability and ops visibility

What to do:

- ranking debug factors
- dashboard views for indexing drift, duplicates, stale/superseded facts

Stop gates:

- [ ] any bad retrieval can be explained from API debug payload
- [ ] benchmark traces include planner + candidate diagnostics

### [ ] 10. Backend/model iteration only after Top-5 gates are stable

What to do:

- compare new backend/model only with locked Top-5 scorecard
- prefer wins that improve Top5/Recall/Hits without Top1 regression

Stop gates:

- [ ] backend/model candidate improves `Top5Accuracy` by >= `5%` relative, or
- [ ] keeps quality flat (`<=1%` delta) with material latency win
- [ ] `Top1Accuracy` regression must stay <= `2%`

## Official Gate Ladder

### Gate A: Smoke Regression

Purpose:

- fast CI sanity
- detect obvious retrieval regressions

Pass rules:

- [ ] tests/build pass
- [ ] `Top5Accuracy` regression <= `2%`
- [ ] `Recall@5` regression <= `2%`

### Gate B: Curated Retrieval Gate (Primary Product Gate)

Purpose:

- decide if retrieval behavior is production-ready

Pass rules:

- [ ] `Top1Accuracy >= 0.45`
- [ ] `Top5Accuracy >= 0.80`
- [ ] `Recall@5 >= 0.75`
- [ ] `MicroRecall@5 >= 0.75`
- [ ] `Hits/Relevant >= 0.75`

### Gate C: Promotion Gate (Backend/Model Changes)

Purpose:

- allow backend/model work only after core retrieval is stable

Pass rules:

- [ ] Gate B is passing
- [ ] candidate change improves `Top5Accuracy` >= `5%` relative, or quality is flat (`<=1%`) with clear latency gain
- [ ] `Top1Accuracy` regression <= `2%`
- [ ] `Recall@5` and `Hits/Relevant` do not regress

## Run Commands For Gates

### Curated retrieval gate run

```bash
scripts/retrieval_quality.sh \
  --fixture test/fixtures/memories.json \
  --eval-set test/fixtures/retrieval_eval.curated.json \
  --top-k 5 \
  --max-queries 0 \
  --embedding-provider ollama \
  --embedding-model all-minilm
```

### Throughput/latency companion run

```bash
scripts/benchmark.sh \
  --fixture test/fixtures/memories_real.json \
  --backend sqlite \
  --search-ops 500 \
  --top-k 5 \
  --embedding-provider ollama \
  --embedding-model all-minilm
```

### Trend history run

```bash
scripts/retrieval_trend.sh \
  --label "curated-top5" \
  --fixture test/fixtures/memories.json \
  --eval-set test/fixtures/retrieval_eval.curated.json \
  --top-k 5 \
  --max-queries 0 \
  --embedding-provider ollama \
  --embedding-model all-minilm
```

## File Tracking Matrix

| File | What to track now | First change |
|---|---|---|
| `scripts/retrieval_quality.sh` | Top-5 quality metrics + hit counts | keep `top_k=5` default and scorecard fields |
| `scripts/retrieval_trend.sh` | trend capture for Top-5 gates | enforce curated Top-5 workflow in examples |
| `scripts/benchmark.sh` | latency/throughput companion benchmark | default to `top_k=5` |
| `BENCHMARKS.MD` | official benchmark policy | production-first Top-5 gate definition |
| `README.md` | operator commands | examples aligned to Top-5 |
| `internal/core/memory/search.go` | ranking/filtering correctness | tune for Top5/Recall/Hits goals |
| `internal/core/memory/store*.go` | canonical memory + indexing correctness | reduce drift and duplication |
| `internal/core/memory/query_routing.go` | planner quality | better route-to-evidence match |
| `internal/repository/sqlite/*` | lifecycle + indexing state | transactional correctness and auditability |

## What Not To Prioritize Right Now

- [ ] score-chasing on paper-only metrics (`F1`, `BLEU`) without Top-5 improvements
- [ ] benchmark-only query tricks not used in production
- [ ] backend/model bake-offs before Gate B stability

## Expected Outcome

If this sequence is followed, Pali will improve on the metrics that map to production behavior:

- can users find the right memory in top 5?
- how often is rank 1 correct?
- how many relevant hits do we actually return?

That is the correct quality bar before deeper backend/model optimization.
