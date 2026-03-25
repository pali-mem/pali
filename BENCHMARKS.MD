# Pali Benchmarks

Pali is infrastructure-first memory software, not a single locked product profile. Out of the box, defaults prioritize reliable local bring-up and operability over peak retrieval quality. Expect tuning (providers, scoring, parser/routing flags, vector backends, and data curation) as part of real deployments.


## Canonical Release Assets

Checked-in release data lives here:

- fixture: `testdata/benchmarks/fixtures/release_memories.json`
- eval set: `testdata/benchmarks/evals/release_curated.json`
- medium fixture: `testdata/benchmarks/fixtures/locomo_medium500.fixture.json`
- medium eval set: `testdata/benchmarks/evals/locomo_medium500.eval.json`
- runnable profiles: `test/benchmarks/profiles/`

Use these wrappers instead of rebuilding commands by hand:

- `test/benchmarks/profiles/release-curated-ollama.sh`
- `test/benchmarks/profiles/release-curated-lexical.sh`
- `test/benchmarks/profiles/release-curated-openrouter.sh`
- `test/benchmarks/profiles/throughput-ollama.sh`
- `test/benchmarks/profiles/throughput-lexical.sh`
- `test/benchmarks/profiles/suite-local.sh`
- `test/benchmarks/profiles/suite-medium-fast.sh`
- `test/benchmarks/profiles/suite-medium-qdrant-openrouter.sh`
- `test/benchmarks/profiles/suite-medium-qdrant-openrouter-parser-graph.sh`
- `test/benchmarks/profiles/suite-qdrant-ollama.sh`

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
- percentile latency compliance (`p50/p95/p99`) via suite `P-score`

Default retrieval tuning now included in benchmark profiles:

- `retrieval.search.adaptive_query_expansion_enabled: false`
- `retrieval.search.candidate_window_multiplier: 5`
- `retrieval.search.early_rerank_base_window: 25`
- `retrieval.search.early_rerank_max_window: 25`

If you run ablations, pin these fields explicitly in scenario args so comparisons stay apples-to-apples.

## Modular Speed Suites

For config-driven infra, compare profiles as paired scenarios instead of reading a single run in isolation.

Use the suite runner:

```bash
python test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.local.json
```

Primary configs:

- local smoke: `test/benchmarks/suites/speed.local.json`
- medium fast lane: `test/benchmarks/suites/speed.medium.fast.json`
- medium qdrant + openrouter lane: `test/benchmarks/suites/speed.medium.qdrant-openrouter.json`
- medium qdrant + openrouter + parser/graph lane: `test/benchmarks/suites/speed.medium.qdrant-openrouter-parser-graph.json`
- qdrant + ollama comparison lane: `test/benchmarks/suites/speed.qdrant_ollama.json`
- optional LoCoMo lane template: `test/benchmarks/suites/speed.locomo.optional.json`

What suites add beyond single-script runs:

- multiple scenarios in one config
- profile-paired comparison blocks (baseline vs candidate)
- one combined scorecard for ingest/search API speed and retrieval metrics
- optional weighted `performance_score` using latency SLO + throughput targets
- warning on non-comparable comparisons (`fixture`, `eval_set`, `top_k` mismatch)

## Local Run Disclosure (Required)

For local benchmark claims, include environment details in the same report/table.
This is required for Ollama/Qdrant runs because hardware and model/runtime versions materially affect results.

Required columns for local benchmark tables:

- `date_utc`
- `os`
- `cpu`
- `ram_gb`
- `gpu`
- `ollama_version`
- `qdrant_version`
- `embedding_model`
- `importance_scorer`
- `dataset` (fixture/eval + counts)
- `metrics` (store/search throughput + p95 + retrieval)

### Current Local Benchmark Host (March 12, 2026)

| Field | Value |
|---|---|
| motherboard | `Gigabyte B650 EAGLE AX` |
| os | `Windows 11 Home (10.0.26200)` |
| cpu | `AMD Ryzen 9 7950X (16 cores / 32 threads)` |
| ram | `33.4 GB` |
| gpus | `NVIDIA GeForce RTX 5070`, `AMD Radeon(TM) Graphics` |
| ollama | `0.17.7` |
| qdrant | `1.17.0` |
| ollama models installed | `all-minilm:latest`, `deepseek-r1:7b`, `qwen2.5:7b`, ... |

### Latest Local Suite Runs (with Device Context)

| date_utc | suite | profile | embedding / scorer | dataset | key metrics |
|---|---|---|---|---|---|
| `2026-03-12` | `speed-medium-fast` | `sqlite + lexical` | `lexical / heuristic` | `locomo_medium500.fixture(500)` + `locomo_medium500.eval(231)` | store `208.909 ops/s`, search `6.216 ops/s`, search `p95=132.340ms`, `performance_score=99.61`, retrieval `Top1=0.320346 Recall@5=0.536436` |
| `2026-03-12` | `speed-medium-fast` | `qdrant + ollama` | `all-minilm / heuristic` | `locomo_medium500.fixture(500)` + `locomo_medium500.eval(231)` | store `98.594 ops/s`, search `6.624 ops/s`, search `p95=126.509ms`, `performance_score=100.00`, retrieval `Top1=0.337662 Recall@5=0.515873` |
| `2026-03-12` | `speed-medium-qdrant-openrouter` | `qdrant + openrouter*` | `openai/text-embedding-3-small:nitro / heuristic*` | `locomo_medium500.fixture(500)` + `locomo_medium500.eval(231)` | store `43.353 ops/s`, search `3.514 ops/s`, search `p95=526.202ms`, `performance_score=73.25`, retrieval `Top1=0.307359 Recall@5=0.537518` |
| `2026-03-12` | `speed-medium-qdrant-openrouter-parser-graph` | `qdrant + openrouter**` | `openai/text-embedding-3-small:nitro / openrouter(gpt-oss-20b:nitro)**` | `locomo_medium500.fixture(500)` + `locomo_medium500.eval(231)` | store `2.436 ops/s`, search `3.231 ops/s`, search `p95=552.008ms`, `performance_score=37.83`, retrieval `Top1=0.263158 Recall@5=0.326673` |

Notes:

- low-eval smoke and scorer reruns were moved to `../pali-results/benchmark-low-eval-archive-20260311T214040`.
- keep this table focused on medium-scale runs (`200-500` records) for positioning claims; smoke runs remain useful for local regression checks only.
- `*` OpenRouter row uses remote API embeddings (`openai/text-embedding-3-small:nitro` via OpenRouter), so network/provider latency and shared-service variability make speed metrics anomalous vs local Ollama/lexical lanes; include as cross-provider reference only.
- `**` Parser+graph row forces `parser.provider=openrouter` and `retrieval.multi_hop.graph_singleton_invalidation=true`; this is a structured-memory stress lane and not directly comparable to heuristic-parser retrieval labels.
- parser+graph retrieval now uses `eval_target=canonical` auto-resolution when parser is enabled.
- prior OpenRouter benchmark (`*`) had `parser.enabled=false`, `parser.provider=heuristic`, and rendered `graph_singleton_invalidation=false`.

## External Benchmark Patterns (What We Borrowed)

Pali suite design mirrors patterns used by leading memory/retrieval stacks:

- Mem0 reports retrieval and QA quality on LOCOMO/LongMemEval plus efficiency deltas (latency, token use), not one single metric:
  - paper: https://arxiv.org/abs/2504.19413
- Zep evaluates against LongMemEval + DMR, and emphasizes both answer quality and latency/cost characteristics:
  - paper: https://arxiv.org/abs/2501.13956
- Google Vertex AI eval guidance separates experiment config from scoring runs and tracks metric outputs per run:
  - docs: https://cloud.google.com/vertex-ai/generative-ai/docs/models/evaluation-overview
- Qdrant benchmark guidance evaluates latency/throughput with quality thresholds (precision-recall tradeoff), not raw speed alone:
  - docs: https://qdrant.tech/benchmarks/

In practice for Pali this means:

- always run profile-paired comparisons (same fixture/eval/top_k) before reading deltas
- keep one lane for API speed (ingest/search latency+throughput) and one lane for retrieval quality
- keep LoCoMo optional but standardized so quality and speed runs are still comparable over time

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

OpenRouter retrieval run (requires `OPENROUTER_API_KEY`):

```bash
bash test/benchmarks/profiles/release-curated-openrouter.sh
```

Local no-dependency throughput run:

```bash
bash test/benchmarks/profiles/throughput-lexical.sh
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

Suite shortcuts:

```bash
make bench-suite
make bench-suite-medium
make bench-suite-qdrant
make bench-suite-openrouter
make bench-suite-openrouter-parser-graph
```

Clean old generated benchmark artifacts:

```bash
make benchmark-clean
```

## Result Layout

Outputs are written to:

- single-run scripts: `test/benchmarks/results/<timestamp>/`
- suite runs: `test/benchmarks/results/suites/<timestamp>-<suite>/`

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

Latest suite runs in this workspace (March 12, 2026):

### Release-Curated Lexical Verification (March 24, 2026)

These are small release-curated verification runs on the checked-in release fixture/eval set (`8` memories, `8` labeled eval queries). They are useful for backend bring-up and regression checks, but they are not medium-scale positioning runs.

| date_utc | backend | run type | key metrics |
|---|---|---|---|
| `2026-03-24` | `sqlite + lexical` | retrieval quality | `Top1=1.000000`, `Recall@5=1.000000`, `MRR=1.000000` |
| `2026-03-24` | `pgvector + lexical` | retrieval quality | `Top1=1.000000`, `Recall@5=1.000000`, `MRR=1.000000` |
| `2026-03-24` | `pgvector + lexical` | API benchmark | store `106.896 ops/s`, store `p95=3.640ms`, search `34.931 ops/s`, search `p95=19.589ms`, batch mode `enabled`, batch fallbacks `0` |

Artifacts:

- sqlite retrieval: `test/benchmarks/results/release-curated-sqlite-lexical-full/20260324T192041Z/`
- pgvector retrieval: `test/benchmarks/results/release-curated-pgvector-lexical-full/20260324T192107Z/`
- pgvector benchmark: `test/benchmarks/results/release-benchmark-pgvector-lexical-full/20260324T192146Z/`

- `speed-medium-fast` (`20260312T045156Z`)
  - artifact: `test/benchmarks/results/suites/20260312T045156Z-speed-medium-fast/`
  - dataset: `locomo_medium500.fixture(500)` + `locomo_medium500.eval(231)`
  - `sqlite + lexical`: store `208.909 ops/s`, search `6.216 ops/s`, search `p95=132.340ms`, `Top1=0.320346`, `Recall@5=0.536436`
  - `qdrant + ollama(all-minilm)`: store `98.594 ops/s`, search `6.624 ops/s`, search `p95=126.509ms`, `Top1=0.337662`, `Recall@5=0.515873`
  - speed delta (`qdrant_ollama` vs `sqlite_lexical`): search `p95 -4.41%`, search throughput `+6.56%`, store throughput `-52.81%`
  - retrieval delta vs prior medium-fast (`20260312T023226Z`): `sqlite Top1 +0.060606 Recall@5 +0.038961`; `qdrant Top1 +0.047619 Recall@5 +0.025974`

- `speed-medium-qdrant-openrouter` (`20260312T025011Z`)
  - artifact: `test/benchmarks/results/suites/20260312T025011Z-speed-medium-qdrant-openrouter/`
  - dataset: `locomo_medium500.fixture(500)` + `locomo_medium500.eval(231)`
  - config state: `parser.enabled=false`, `parser.provider=heuristic`, rendered `graph_singleton_invalidation=false`
  - `qdrant + openrouter(openai/text-embedding-3-small:nitro)`: store `43.353 ops/s`, search `3.514 ops/s`, search `p95=526.202ms`, `performance_score=73.25`
  - retrieval: `Top1=0.307359`, `Recall@5=0.537518`, `nDCG@5=0.433679`, `MRR=0.410317`

- `speed-medium-qdrant-openrouter-parser-graph` (`20260312T033613Z`)
  - artifact: `test/benchmarks/results/suites/20260312T033613Z-speed-medium-qdrant-openrouter-parser-graph/`
  - dataset: `locomo_medium500.fixture(500)` + `locomo_medium500.eval(231)`
  - config state: `parser.enabled=true`, `parser.provider=openrouter`, `parser.openrouter_model=openai/gpt-oss-20b:nitro`, rendered `graph_singleton_invalidation=true`
  - speed: store `2.436 ops/s`, search `3.231 ops/s`, search `p95=552.008ms`, `performance_score=37.83`
  - retrieval (`eval_target=canonical`): `Top1=0.263158`, `Recall@5=0.326673`, `nDCG@5=0.299248`, `MRR=0.372807`

Older low-eval and smoke runs were archived under:

- `../pali-results/benchmark-low-eval-archive-20260311T214040`

### Full LoCoMo Retrieval (Top Retained Runs)

Dataset for both rows: `research/data/locomo10.paperlite.fixture.json` (`5882`) + `research/data/locomo10.paperlite.eval.json` (`1533`), `top_k=10`.

| date_utc | run | profile | Top1 | Hit@10 | Recall@10 | nDCG@10 | MRR | store audit |
|---|---|---|---:|---:|---:|---:|---:|---|
| `2026-03-12` | `locomo.full.no-parser.routed.v1` | `qdrant + ollama(all-minilm), parser=off, routing/rerank/temporal/kind=on` | `0.2909` | `0.6491` | `0.5822` | `0.4261` | `0.3996` | memories `5882`, generic_qv `0.00%`, scaffold `0.00%` |
| `2026-03-12` | `locomo.full` | `qdrant + ollama(all-minilm), parser=off` | `0.2629` | `0.6504` | `0.5803` | `0.4104` | `0.3786` | memories `5882`, generic_qv `0.00%`, scaffold `0.00%` |

Artifacts:

- `test/benchmarks/results/manual-locomo-full-check/locomo.full.json`
- `test/benchmarks/results/manual-locomo-full-check/locomo.full.no-parser.routed.v1.json`

Default decision evidence (full dataset, same fixture/eval/top_k):

- `Top1`: `0.2629 -> 0.2909` (`+0.0280`)
- `Recall@10`: `0.5803 -> 0.5822` (`+0.0019`)
- `MRR`: `0.3786 -> 0.3996` (`+0.0210`)
- This A/B is why routing/rerank/temporal/kind are now benchmark defaults.

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
