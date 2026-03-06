# 2026-03-05 — Paperlite Retrieval/Context Tuning (Hybrid Only)

## Goal

Address the main bottlenecks observed in LOCOMO paperlite runs:

1. retrieval/context quality too low
2. Recall@60 too low for evidence coverage

Run only one mode for faster iteration:

- `embedding-provider=ollama` (hybrid dense + lexical fusion in current Pali search flow)

## What Changed

### 1) Multi-query retrieval + fusion in evaluator

File:

- `research/eval_locomo_f1_bleu.py`

Changes:

- Added query variant generation per question:
  - original question
  - compact keyword form (stopword-pruned)
  - tail-keyword variant
- Added reciprocal-rank-fusion (RRF) over variant search results:
  - `--retrieval-query-variants` (default `3`)
  - `--retrieval-rrf-k` (default `60`)

### 2) Context expansion around retrieved dialogue turns

File:

- `research/eval_locomo_f1_bleu.py`

Changes:

- Added dialog-id parsing from paperlite fixture text (`[dialog:Dx:y]`).
- Added optional neighbor-turn expansion for answer context:
  - `--context-neighbor-window` (default `1`)
  - `--context-max-items` (default `24`)

### 3) Paperlite converter options

File:

- `research/prepare_locomo_eval.py`

Changes:

- Added `--mode paperlite` to include sample/dialog/time/speaker metadata in memory content.
- Added `--sanitize-percent` for robust downstream handling.

## Tuned Run Command

```bash
python3 research/eval_locomo_f1_bleu.py \
  --fixture research/data/locomo10.paperlite.fixture.json \
  --eval-set research/data/locomo10.paperlite.eval.json \
  --embedding-provider ollama \
  --embedding-model all-minilm \
  --top-k 60 \
  --max-queries 200 \
  --answer-mode generate \
  --answer-model qwen2.5:3b \
  --answer-top-docs 8 \
  --retrieval-query-variants 3 \
  --retrieval-rrf-k 60 \
  --context-neighbor-window 1 \
  --context-max-items 24 \
  --out-json research/results/paperlite_tuned/20260305T063035Z/ollama_tuned.json \
  --out-summary research/results/paperlite_tuned/20260305T063035Z/ollama_tuned.summary.txt
```

## Before/After (Hybrid, 200 queries, top_k=60)

Baseline artifact:

- `research/results/paperlite/20260305T044708Z/ollama.json`

Tuned artifact:

- `research/results/paperlite_tuned/20260305T063035Z/ollama_tuned.json`

### Retrieval metrics

- Recall@60:
  - baseline: `0.197667`
  - tuned: `0.234333`
  - delta: `+0.036667`
- nDCG@60:
  - baseline: `0.092308`
  - tuned: `0.112709`
  - delta: `+0.020401`
- MRR:
  - baseline: `0.069109`
  - tuned: `0.086149`
  - delta: `+0.017039`

### Generated QA lexical metrics

- F1 (paper scale):
  - baseline: `3.54`
  - tuned: `2.78`
  - delta: `-0.75`
- BLEU-1 (paper scale):
  - baseline: `2.74`
  - tuned: `2.13`
  - delta: `-0.62`

## Interpretation

- Retrieval improved as intended.
- Generated answer quality regressed.
- Most likely cause: neighbor context expansion introduced extra noise for the answer model prompt.

## Immediate Next Tuning (recommended)

1. Keep multi-query retrieval enabled (`variants=3`) because retrieval improved.
2. Disable/limit neighbor expansion (`--context-neighbor-window 0`) and rerun.
3. Retune answer context budget (`--answer-top-docs`, `--context-max-items`) to reduce distractors.

---

## Follow-up Ablation (same day)

Question tested:

- Can we keep retrieval gains but recover generated QA quality by removing context-neighbor expansion?

Run (hybrid only):

- retrieval variants kept: `3`
- neighbor expansion disabled: `--context-neighbor-window 0`
- tighter answer context: `--answer-top-docs 4`, `--context-max-items 12`

Artifact:

- `research/results/paperlite_ablation/20260305T065539Z/ollama_ablation.json`

### Ablation vs Baseline (hybrid)

Baseline:

- `research/results/paperlite/20260305T044708Z/ollama.json`

Ablation:

- Recall@60: `0.232667` (vs `0.197667`, `+0.035000`)
- nDCG@60: `0.108875` (vs `0.092308`, `+0.016567`)
- MRR: `0.081675` (vs `0.069109`, `+0.012566`)
- F1 generated (paper scale): `3.55` (vs `3.54`, `+0.02`)
- BLEU-1 generated (paper scale): `2.91` (vs `2.74`, `+0.17`)

Conclusion:

- Multi-query retrieval gives consistent retrieval gains.
- Neighbor expansion was the part hurting generated QA.
- Better short-term config: keep query variants, disable neighbor expansion, use tighter answer context.

---

## Fast Iteration with Cached DB

To avoid re-storing ~5.8k memories each run, evaluator now supports persisted store artifacts:

- `--db-path`
- `--index-map-path`
- `--reuse-existing-store`
- `--reset-db`

### First run (build cache)

```bash
python3 research/eval_locomo_f1_bleu.py ... \
  --db-path research/cache/paperlite.sqlite \
  --index-map-path research/cache/paperlite_idx_map.json \
  --reset-db
```

### Subsequent runs (skip ingestion)

```bash
python3 research/eval_locomo_f1_bleu.py ... \
  --db-path research/cache/paperlite.sqlite \
  --index-map-path research/cache/paperlite_idx_map.json \
  --reuse-existing-store
```
