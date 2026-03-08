# Pali Benchmarks

> Status: redesigned for production retrieval validation (Top-5 first) as of 2026-03-07.

## Benchmark Objective

Pali benchmarking now optimizes for production retrieval quality on our own data, not paper-style answer metrics.

Primary question:
- When a user asks a real query, do we return the right memory in the top 5, and how many relevant memories do we actually return?

## Official Scorecard (Top-5)

All official quality gates now use `top_k=5` and these metrics:

- `Top1Accuracy`: relevant hit at rank 1 (`top1_hit_rate`)
- `Top5Accuracy`: any relevant hit in top 5 (`hit_rate_at_k`)
- `Recall@5`: per-query average recall (`recall_at_k`)
- `MicroRecall@5`: corpus-level recall (`micro_recall_at_k`)
- `Hits@5`: total returned relevant hits (`total_hits_at_k`)
- `Hits/Relevant`: `total_hits_at_k / total_relevant`

Secondary diagnostics (not promotion gates):
- `nDCG@5`
- `MRR`
- latency and throughput from `scripts/benchmark.sh`

## Data Policy

Production scorecards must run on internal/curated data, not synthetic-prefix-only queries.

Use this priority order:

1. `test/fixtures/retrieval_eval.curated.json` with `test/fixtures/memories.json`
2. Larger curated internal eval sets (same format)
3. Auto-generated prefix eval only for smoke checks

Paperlite/LOCOMO-style evaluations are retained as research diagnostics, not release gates.

## Reproducible Runs

### Prerequisites

```bash
go run ./cmd/setup
scripts/bench_setup.sh
```

### Production Retrieval Gate Run (default)

```bash
scripts/retrieval_quality.sh \
  --fixture test/fixtures/memories.json \
  --eval-set test/fixtures/retrieval_eval.curated.json \
  --top-k 5 \
  --max-queries 0 \
  --embedding-provider ollama \
  --embedding-model all-minilm
```

### Throughput + Latency Run

```bash
scripts/benchmark.sh \
  --fixture test/fixtures/memories_real.json \
  --backend sqlite \
  --search-ops 500 \
  --top-k 5 \
  --embedding-provider ollama \
  --embedding-model all-minilm
```

### Trend Tracking

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

## Gate Definitions

### Gate A: Smoke (PR checks)

Pass rules:
- tests/build pass
- `Top5Accuracy` does not regress by more than `2%`
- `Recall@5` does not regress by more than `2%`

### Gate B: Curated Retrieval Quality (main product gate)

Pass rules:
- `Top1Accuracy >= 0.45`
- `Top5Accuracy >= 0.80`
- `Recall@5 >= 0.75`
- `MicroRecall@5 >= 0.75`
- `Hits/Relevant >= 0.75`

### Gate C: Promotion Gate (backend/model changes)

Promotion allowed only if Gate B passes and:
- `Top5Accuracy` improves by at least `5%` relative or stays within `1%` with clear latency win
- `Top1Accuracy` does not regress by more than `2%`
- `Recall@5` does not regress

## Result Files

Outputs are written to:

- `test/benchmarks/results/*.json`
- `test/benchmarks/results/*.summary.txt`
- `test/benchmarks/trends/retrieval_quality_history.jsonl`

Required fields for reviews:

- fixture path + hash
- eval-set path + hash
- `top_k` (must be `5` for official gates)
- metric block including top1/top5/recall/hits
- git commit hash (trend runs)

## Notes

- Existing historical `Top-10` results remain valid as history, but are no longer the official gate target.
- If a run uses `top_k != 5`, label it explicitly as exploratory.
