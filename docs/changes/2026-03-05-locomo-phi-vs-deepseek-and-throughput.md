# 2026-03-05 LOCOMO Re-run: Phi vs DeepSeek + Throughput Reality

## Why

Run one fresh LOCOMO benchmark after the P1 + Phase 5 updates, compare against the prior baseline, and explain why this workload does not show mock-benchmark-level TPS.

## Runs used

- Baseline (prior):
  - `/tmp/locomo_structured_hybrid_120_phi.json`
- Current run (phi4-mini):
  - `research/results/p1_parser_benchmark/20260305T204018Z/ollama.json`
  - `research/results/p1_parser_benchmark/20260305T204018Z/ollama.summary.txt`
- Current run (deepseek-r1:7b):
  - `research/results/p1_parser_benchmark/20260305T211801Z/ollama.json`
  - `research/results/p1_parser_benchmark/20260305T211801Z/ollama.summary.txt`

## Metric deltas

### Baseline -> Current (phi4-mini)

| Metric | Baseline | Phi | Delta |
|---|---:|---:|---:|
| F1 generated | 0.122195 | 0.122589 | +0.000394 |
| BLEU-1 generated | 0.088735 | 0.089437 | +0.000702 |
| Recall@60 | 0.012500 | 0.386111 | +0.373611 |
| nDCG@60 | 0.002751 | 0.106613 | +0.103861 |
| MRR | 0.000642 | 0.041811 | +0.041169 |

### Baseline -> Current (deepseek-r1:7b)

| Metric | Baseline | DeepSeek | Delta |
|---|---:|---:|---:|
| F1 generated | 0.122195 | 0.107346 | -0.014850 |
| BLEU-1 generated | 0.088735 | 0.079465 | -0.009270 |
| Recall@60 | 0.012500 | 0.377778 | +0.365278 |
| nDCG@60 | 0.002751 | 0.104875 | +0.102124 |
| MRR | 0.000642 | 0.041432 | +0.040790 |

### Phi -> DeepSeek (current code)

| Metric | Phi | DeepSeek | Delta |
|---|---:|---:|---:|
| F1 generated | 0.122589 | 0.107346 | -0.015244 |
| BLEU-1 generated | 0.089437 | 0.079465 | -0.009972 |
| Recall@60 | 0.386111 | 0.377778 | -0.008333 |
| nDCG@60 | 0.106613 | 0.104875 | -0.001738 |
| MRR | 0.041811 | 0.041432 | -0.000379 |

## Why score changed so much

1. Retrieval alignment changed materially, not just answer generation.
   - Parser-enabled ingest + parser-safe fixture-index grouping moved retrieval from near-zero (`Recall@60=0.0125`) to ~`0.38`.
2. Noise suppression reduced repeated high-frequency derived text in retrieval candidates.
3. Short high-signal parser facts are now retained, improving evidence coverage.
4. Generated QA moved less than retrieval because this profile is still mostly extractive-first.

## Throughput: why not ~1000 TPS here

The ~`924 ops/s` result from `scripts/benchmark.sh` is a mock benchmark and not comparable to LOCOMO parser ingest.

This LOCOMO workload includes:
- real Ollama embedding calls,
- parser fan-out writes per turn (raw + facts),
- per-fact dedupe/search checks,
- full QA evaluation/generation after ingest.

Observed for full LOCOMO runs:
- Phi run wall time: start `20260305T204018Z`, summary mtime `2026-03-05T21:04:53Z` (~1475s total)
- DeepSeek run wall time: `real 1760.04s`

With 5882 fixture turns, this is single-digit turns/sec end-to-end, which is expected for parser-heavy + Ollama-backed runs.

## Validity note

A prior deepseek attempt using `--reuse-existing-store` after an interrupted `--reset-db` run produced invalid retrieval (`0.0`) due partial DB state and was discarded. The metrics above come from the clean rerun at `20260305T211801Z`.
