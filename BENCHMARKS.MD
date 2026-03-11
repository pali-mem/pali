# Pali Benchmarks

Pali has two benchmark lanes:

- release benchmarks on a checked-in canonical fixture and eval set
- research benchmarks on larger external datasets such as LOCOMO

Only the release lane should drive v0.1 defaults and go or no-go decisions.

## Canonical Release Assets

Checked-in release data lives here:

- fixture: `testdata/benchmarks/fixtures/release_memories.json`
- eval set: `testdata/benchmarks/evals/release_curated.json`
- runnable profiles: `test/benchmarks/profiles/`

Use these wrappers instead of rebuilding commands by hand:

- `test/benchmarks/profiles/release-curated-ollama.sh`
- `test/benchmarks/profiles/release-curated-lexical.sh`
- `test/benchmarks/profiles/throughput-ollama.sh`

Every benchmark run now copies both config inputs into the result directory:

- `config.profile.yaml`
- `config.rendered.yaml`

That means results are tied to the exact runtime configuration used, not just the fixture and eval hashes.

## Official Scorecard

Official retrieval gates use `top_k=5` and these metrics:

- `Top1HitRate`
- `Top5Accuracy`
- `Recall@5`
- `MicroRecall@5`
- `Hits/Relevant`
- `nDCG@5` and `MRR` as diagnostics

Throughput runs track:

- store throughput and latency
- search throughput and latency
- batch-store fallback behavior

## Recommended Commands

Primary release-quality run:

```bash
bash test/benchmarks/profiles/release-curated-ollama.sh
```

Local smoke or CI-friendly run:

```bash
bash test/benchmarks/profiles/release-curated-lexical.sh
```

Throughput run:

```bash
bash test/benchmarks/profiles/throughput-ollama.sh
```

Direct script form:

```bash
scripts/retrieval_quality.sh \
  --fixture testdata/benchmarks/fixtures/release_memories.json \
  --eval-set testdata/benchmarks/evals/release_curated.json \
  --top-k 5 \
  --max-queries 0 \
  --embedding-provider ollama \
  --embedding-model all-minilm
```

## Result Layout

Outputs are written to `test/benchmarks/results/<timestamp>/`.

Important files per run:

- `benchmark.json`
- `benchmark.summary.txt`
- `retrieval_quality.json`
- `retrieval_quality.summary.txt`
- `config.profile.yaml`
- `config.rendered.yaml`
- `trace.json` when retrieval tracing is available

Trend history is appended to:

- `test/benchmarks/trends/retrieval_quality_history.jsonl`

Each trend row now records:

- fixture path and hash
- eval-set path and hash
- config profile path and hash
- rendered config path and hash
- commit hash

## Latest Retained Runs

Latest canonical run in this workspace was on March 11, 2026 using the checked-in release fixture and eval set:

- canonical retrieval run `20260311T012253Z`
  - backend: `sqlite`
  - embedding: `lexical`
  - config profile: `test/config/providers/lexical.yaml`
  - `Top1HitRate=1.000000`
  - `Top5Accuracy=1.000000`
  - `Recall@5=1.000000`
- canonical throughput run `20260311T012253Z`
  - backend: `sqlite`
  - embedding: `lexical`
  - store throughput: `22.735 ops/sec`
  - search throughput: `7.539 ops/sec`

This is a smoke-quality baseline, not the final product-quality baseline. It proves the checked-in fixture, eval set, profile wrappers, config capture, and result format are working end to end without external model dependencies.

Historical research context retained from March 9, 2026:

- LOCOMO raw retrieval run `20260309T050555Z`
  - backend: `qdrant`
  - embedding: `lexical`
  - `Top1HitRate=0.275510`
  - `Top5Accuracy=0.469388`
  - `Recall@5=0.391156`

## Gate Rules

PR smoke expectations:

- tests and build pass
- docs freshness passes
- release benchmark profile is reproducible

Release expectations:

- run `scripts/release_gate.sh`
- run at least one curated retrieval benchmark profile
- attach the result directory or metric summary to the release note or changelog entry

## What Not To Treat As Canonical

Do not use these as release truth:

- ad hoc files under `test/benchmarks/results/`
- temp SQLite files and logs
- generated fixture dumps under ignored directories
- research-only LOCOMO runs unless explicitly called out as research
