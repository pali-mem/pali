# Research

This folder is a sandbox for benchmark experiments that should not modify core benchmark scripts.

## Paper Mapping

Paper: [arXiv:2504.19413](https://arxiv.org/abs/2504.19413)  
Core evaluation style in the paper uses a labeled dataset and reports lexical answer metrics (`F1`, `BLEU-1`) plus `LLM-as-a-Judge`.

For now, this repo tracks a **no-judge** adaptation:

- Keep existing benchmark scripts unchanged.
- Use a labeled retrieval workload when available.
- Compare `ollama` vs `lexical` on retrieval metrics (`Top1HitRate`, `Recall@K`, `nDCG@K`, `MRR`).
- Score whether the benchmark itself is good enough for regression tracking.

## Files

- `run_ollama_vs_lexical.sh`: runs retrieval benchmark matrix (`ollama`, `lexical`) via `scripts/retrieval_quality.sh`.
- `analyze_benchmark_quality.sh`: checks coverage, curation, and discriminative power; writes JSON + Markdown report.
- `prepare_locomo_eval.py`: converts LOCOMO dialog + QA evidence into Pali fixture/eval formats.
- `run_locomo_paper_style.sh`: end-to-end LOCOMO download/convert/run flow.
- `results/` (gitignored): per-run artifacts.
- `data/` (gitignored): downloaded and converted dataset artifacts.

## Run

```bash
research/run_ollama_vs_lexical.sh \
  --fixture test/fixtures/memories_real_phi4_combined.json \
  --max-queries 200 \
  --top-k 10 \
  --ollama-model all-minilm
```

## LOCOMO (Closer to Mem0 Paper)

```bash
research/run_locomo_paper_style.sh \
  --top-k 10 \
  --max-queries -1 \
  --ollama-model all-minilm
```

This uses LOCOMO QA questions and evidence labels as eval ground truth.  
It still evaluates retrieval quality only (no answer-generation `F1/BLEU-1`, no LLM judge).

Optional curated eval-set:

```bash
research/run_ollama_vs_lexical.sh \
  --fixture test/fixtures/memories_real_phi4_combined.json \
  --eval-set test/fixtures/retrieval_eval.sample.json
```

## Interpreting "Benchmark Quality"

The analyzer reports:

- `coverage_ok`: enough evaluated queries.
- `curated_eval_set`: query labels are externally specified (not auto-first-words).
- `discriminative_ok`: metrics differ enough between providers to detect regressions.
- `query_leakage_risk`: high when auto-generated short prefix queries are used.

This gives a practical answer to: "Are our benchmarks good enough yet?" without adding LLM judge scoring.
