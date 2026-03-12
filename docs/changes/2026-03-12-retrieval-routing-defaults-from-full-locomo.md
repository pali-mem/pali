# 2026-03-12: Retrieval Routing Defaults from Full LoCoMo

## What Changed

Default retrieval behavior is now standardized around the no-parser full-dataset winner:

- `retrieval.answer_type_routing_enabled: true`
- `retrieval.early_rank_rerank_enabled: true`
- `retrieval.temporal_resolver_enabled: true`
- benchmark LoCoMo lane defaults now include:
  - `--retrieval-kind-routing`
  - `--retrieval-answer-type-routing`
  - `--retrieval-early-rank-rerank`
  - `--retrieval-temporal-resolver`

Applied in:

- `internal/config/defaults.go`
- `internal/core/memory/service.go` (service fallback defaults)
- `pali.yaml.example` (generated config template)
- `test/benchmarks/suites/speed.locomo.optional.json`
- `research/eval_locomo_f1_bleu.py` (LoCoMo benchmark harness defaults)

## Why

On the full LoCoMo paperlite dataset, no-parser retrieval improved when routing/rerank/temporal/kind were enabled together, with the same fixture/eval/top_k and backend/model.

This gave stronger first-hit behavior (`Top1`) and rank quality (`MRR`) without requiring parser expansion.

## Evidence

Dataset:

- fixture: `research/data/locomo10.paperlite.fixture.json` (`5882`)
- eval set: `research/data/locomo10.paperlite.eval.json` (`1533`)
- backend/model: `qdrant + ollama(all-minilm)`
- `top_k=10`

Baseline (`locomo.full`, parser off):

- Top1: `0.2629`
- Hit@10: `0.6504`
- Recall@10: `0.5803`
- nDCG@10: `0.4104`
- MRR: `0.3786`

Candidate (`locomo.full.no-parser.routed.v1`, parser off + routing/rerank/temporal/kind on):

- Top1: `0.2909`
- Hit@10: `0.6491`
- Recall@10: `0.5822`
- nDCG@10: `0.4261`
- MRR: `0.3996`

Delta (candidate - baseline):

- Top1: `+0.0280`
- Hit@10: `-0.0013`
- Recall@10: `+0.0019`
- nDCG@10: `+0.0157`
- MRR: `+0.0210`

## Artifacts

- `test/benchmarks/results/manual-locomo-full-check/locomo.full.json`
- `test/benchmarks/results/manual-locomo-full-check/locomo.full.no-parser.routed.v1.json`
