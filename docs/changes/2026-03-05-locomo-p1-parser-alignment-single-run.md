# 2026-03-05 LOCOMO P1 Parser Alignment (Single Run)

## Why

Validate the P1-first changes (noise suppression, parser alignment, parser-safe eval mapping) with one fresh LOCOMO run and compare against the prior 120-query baseline.

## What changed in this run

- Removed participant boilerplate derivation (`Conversation participants: ...`) from structured observations.
- Added legacy dual-write dedupe guard (`DualWriteDedupeThreshold = 0.92`) with negation safety.
- Updated heuristic parser short-fact handling to allow high-signal short values (dates/numbers/status/identity/occupation list).
- Ran parser-enabled ingestion path (heuristic parser) and disabled dual-write flags in this run profile.
- Updated evaluator category naming and parser-safe evidence grouping (`index -> [memory_ids]`) for retrieval scoring.

## Run setup

- Candidate run:
  - `research/results/p1_parser_benchmark/20260305T184048Z/ollama.json`
  - `research/results/p1_parser_benchmark/20260305T184048Z/ollama.summary.txt`
  - `research/results/p1_parser_benchmark/20260305T184048Z/ollama.trace.jsonl`
- Baseline run:
  - `/tmp/locomo_structured_hybrid_120_phi.json`
  - `/tmp/locomo_structured_hybrid_120_phi.txt`
- Shared settings:
  - `max_queries=120`, `top_k=60`
  - embedding provider/model: `ollama / all-minilm`
  - answer mode/model: `hybrid / phi4-mini:latest`
  - extractive threshold: `0.42`
  - temporal prefer + kind routing + structured memory enabled

## Metric deltas (baseline -> candidate)

| Metric | Baseline | Candidate | Delta |
|---|---:|---:|---:|
| F1 generated | 0.122195 | 0.123423 | +0.001227 |
| BLEU-1 generated | 0.088735 | 0.089993 | +0.001258 |
| Recall@60 | 0.012500 | 0.377778 | +0.365278 |
| nDCG@60 | 0.002751 | 0.105571 | +0.102819 |
| MRR | 0.000642 | 0.042129 | +0.041487 |

Comparison artifact:
- `research/results/p1_parser_benchmark/20260305T184048Z/comparison_vs_tmp120.json`

## Category F1 deltas

- Multi-hop: `0.043367 -> 0.043367` (`+0.000000`)
- Temporal: `0.262462 -> 0.268468` (`+0.006006`)
- Open-domain: `0.086875 -> 0.078217` (`-0.008658`)
- Single-hop: `0.064691 -> 0.065186` (`+0.000495`)

## What likely changed the score the most

1. Retrieval quality improved after removing high-frequency derived noise and stopping duplicate structured writes.
2. Parser-enabled ingestion captured more short, high-signal facts that were previously filtered.
3. Evaluator retrieval mapping is now parser-safe (`index -> multiple memory ids`), which improves alignment between expected evidence and actual parser writes.

Generated QA F1/BLEU moved only slightly because answer-layer behavior was already mostly extractive in this profile; the big shift is concentrated in retrieval metrics.

## Notes on comparability

- Baseline used legacy structured dual-write behavior and no parser block in generated eval config.
- Candidate uses parser-enabled flow and parser-safe retrieval grouping, so retrieval deltas include both system and eval-alignment effects.
