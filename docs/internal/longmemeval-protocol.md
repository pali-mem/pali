# LongMemEval-S Evaluation Protocol (proposal, 2026-10-25)

Status: proposal. Nothing here has been run yet.

## Why

Every Pali number to date is retrieval-only (Recall@k, nDCG, MRR) or extractive F1 on LoCoMo. No run measures
answer accuracy with an LLM judge, so no Pali number is comparable to published Mem0 / Zep / Honcho / Mastra
figures. One honest, reproducible LongMemEval-S run fixes that. The goal is a number with a published method,
whatever it turns out to be.

## Dataset

- LongMemEval-S (500 questions, ~115k-token histories per question), from the official LongMemEval release.
  Record the dataset commit/hash in the results.
- Not checked into this repo (size and license). The harness downloads it into `research/data/` (gitignored).

## Systems under test

Each run is one row in the results table. Same reader and judge across all rows.

| row | ingest | read path |
|---|---|---|
| full-context baseline | none | entire history in the prompt (truncate to reader context if needed, and say so) |
| pali-lexical | raw turns, `embedding.provider: lexical` | top-k `/v1/memory/search` |
| pali-dense | raw turns, a named embedding model | top-k search |
| pali-parser | `parser.enabled: true` with a named LLM | top-k search |
| pali-observational | observational mode (if built) | observation log |

One tenant per question, so histories never mix.

## Fixed parameters (check in with results)

- `top_k`: 20 (also report 10 and 60 if cheap).
- Reader: a named `gpt-4o-mini`-class model, temperature 0, fixed prompt checked in at
  `research/prompts/longmemeval_reader.txt`.
- Judge: the official LongMemEval judge prompt and the judge model the LongMemEval paper uses, temperature 0.
  If a different judge is used, the results must say so prominently.
- Pali config: the rendered `pali.yaml` for each row, copied into the results directory (the existing
  benchmark scripts already do this via `cmd/configrender`).
- Seed, Pali git commit, dataset hash, model IDs, and date.

## Outputs

- `results/longmemeval/<date>-<row>/`: rendered config, per-question JSONL (question id, type, retrieved
  memory ids, reader answer, judge verdict, latency), and a summary JSON.
- Summary table: overall accuracy, accuracy per LongMemEval question type, mean retrieval latency, mean
  context tokens sent to the reader.
- A short write-up in `docs/benchmarks.md` with the table, the fixed parameters, and the caveat list below.

## Caveats to publish with the number

- Judge model and prompt; any deviation from the official protocol.
- Reader model; published systems use different readers, which moves accuracy by 10+ points.
- Whether the full-context baseline was truncated.
- Single run, no confidence interval unless repeated; with n=500, a binomial 95% CI is roughly ±4 points.

## Cost estimate (to confirm before running)

Rough order: 500 questions × (reader call + judge call) per row, plus ingest-time LLM calls for the parser and
observational rows (one call per turn batch). The parser and observational rows dominate. Price it with the
chosen models before running; do not run the parser row on the full set until a 50-question pilot works.

## Open questions (maintainer decision)

1. Reader and judge model choice, and budget ceiling.
2. Whether to publish rows that look bad (recommended: yes; that is the point of the run).
