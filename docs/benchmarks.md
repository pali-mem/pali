# Benchmarks

What Pali's benchmarks measure, how they are run, and what the current numbers are. Every number below is copied from a checked-in file or a dated maintainer summary, and the source is named next to it.

## Read this first

- **No benchmark run uses an LLM judge.** Retrieval is scored against labeled evidence. Answer quality, where measured, is extractive token F1 against the reference answer.
- **These numbers are not comparable to published Mem0, Zep, Honcho or similar accuracy figures.** Those report LLM-judged answer accuracy (LoCoMo or LongMemEval) with their own readers, judges and rerankers. Pali has not run LongMemEval-S end to end and has no judged accuracy number.
- **Sample sizes are small.** The smallest lane has 8 queries and the largest has 1533. Differences of a few hundredths on the 200-query lane are within noise.
- **Retrieval recall is the weak point.** Recall@10 on LoCoMo is 0.445 on the lexical 200-question lane and 0.58 on the full set with Ollama embeddings (tables below). Multi-hop extractive F1 was 1.26 on 39 questions in the v0.2.1 run.

## Methodology

### Datasets

| Set | Fixture (memories) | Eval set (queries) | Used for |
|---|---|---|---|
| Release curated | `testdata/benchmarks/fixtures/release_memories.json` (8) | `testdata/benchmarks/evals/release_curated.json` (8) | Bring-up and regression smoke checks. Not a quality claim. |
| LoCoMo medium | `testdata/benchmarks/fixtures/locomo_medium500.fixture.json` (500) | `testdata/benchmarks/evals/locomo_medium500.eval.json` (231) | Speed and retrieval suites. |
| LoCoMo full | converted from the public LoCoMo release by `research/prepare_locomo_eval.py` (5882 memories) | 1533 labeled questions | Research runs. The converted files are gitignored and are not in the repo. |

Counts for the checked-in files were read from the files; counts for the full set come from [BENCHMARKS.MD](https://github.com/pali-mem/pali/blob/main/BENCHMARKS.MD).

### Lanes

Runs differ by embedding provider and by who parses memories at write time. Compare only runs from the same lane.

- **Lexical.** `embedding.provider=lexical`: no embedding model, SQLite FTS5 BM25 plus Pali's reweighting. The default and the only lane that needs no external service. Its "dense" route is not a real embedding.
- **Ollama.** Local embeddings, usually `all-minilm`, often with Qdrant as the vector store.
- **LLM parser.** `parser.enabled=true` with an Ollama or OpenRouter model extracting structured facts at write time. Slow on ingest and, in the retained runs below, not better on the medium suite.
- **Heuristic parser.** The default rule-based parser, no LLM.

Profiles live in `test/benchmarks/profiles/` and suites in `test/benchmarks/suites/`. Each run copies `config.profile.yaml` and `config.rendered.yaml` next to its results so a number is tied to the exact configuration.

### Metrics

- **Retrieval** (the official scorecard): `Top1HitRate`, `Top5Accuracy`, `Recall@K`, `MicroRecall@K`, with `nDCG@K` and `MRR` as diagnostics. Release gates use `top_k=5`; the LoCoMo full runs use `top_k=10`. Evidence labels come from the dataset, so no judge is involved.
- **Extractive F1.** Token F1 between the answer Pali extracts from the retrieved memories and the reference answer (lowercased tokens, bag overlap), implemented in `research/eval_locomo_f1_bleu.py`. `--answer-mode` can also be `generate` or `hybrid`; those add an LLM reader and the rows say so. F1 is reported on a 0 to 100 scale.
- **Speed.** Store and search throughput and p95 latency, from `benchmark_suite.py`.

### Rules for comparing runs

- Same fixture, eval set and `top_k`. The suite runner warns when these differ.
- Pin the retrieval tuning fields in scenario args when running ablations.
- Local runs must record date, OS, CPU, RAM, GPU, Ollama and Qdrant versions, embedding model, scorer and dataset sizes. The full requirements are in [BENCHMARKS.MD](https://github.com/pali-mem/pali/blob/main/BENCHMARKS.MD).

## Current numbers

### Latest change measurement: v0.2.1 (2026-10-25)

Lexical embeddings, heuristic parser, LoCoMo paperlite sample of 200 questions, `top_k=10`, extractive answers, seed 1337. Source: `changelog/v0.2.1.md`.

| Metric | Before | After |
|---|---|---|
| Recall@5 | 0.257 | 0.255 |
| Recall@10 | 0.444 | 0.445 |
| nDCG@10 | 0.264 | 0.269 |
| MRR | 0.238 | 0.245 |
| F1 (extractive) | 7.71 | 7.66 |

Recall did not change and ordering inside the top 10 improved slightly. F1 is flat within noise at n=200. This is the most recent measurement and uses the current code; the tables below predate those ranking fixes.

### Medium suite (2026-03-12)

LoCoMo medium (500 memories, 231 queries). Source: `BENCHMARKS.MD`, suite `speed-medium-fast` (run `20260312T045156Z`) and the two OpenRouter suites.

| Profile | Top1 | Recall@5 | nDCG@5 | MRR | Search p95 |
|---|---|---|---|---|---|
| sqlite + lexical | 0.320 | 0.536 | n/a | n/a | 132 ms |
| Qdrant + Ollama (all-minilm) | 0.338 | 0.516 | n/a | n/a | 127 ms |
| Qdrant + OpenRouter embeddings | 0.307 | 0.538 | 0.434 | 0.410 | 526 ms (remote API) |
| Qdrant + OpenRouter embeddings + LLM parser + graph | 0.263 | 0.327 | 0.299 | 0.373 | 552 ms |

The OpenRouter latency includes network time and is not comparable to the local rows. The last row is a structured-memory stress lane scored against the canonical target; it was worse than the plain lanes and the cause (bug or prompt) is unresolved. Hardware for these runs is listed in `BENCHMARKS.MD` (Ryzen 9 7950X, RTX 5070, Ollama 0.17.7, Qdrant 1.17.0, Windows 11).

### LoCoMo full retrieval (2026-03-12)

5882 memories, 1533 queries, `top_k=10`, Qdrant + Ollama (all-minilm), parser off. Source: `BENCHMARKS.MD`.

| Run | Top1 | Hit@10 | Recall@10 | nDCG@10 | MRR |
|---|---|---|---|---|---|
| default | 0.2629 | 0.6504 | 0.5803 | 0.4104 | 0.3786 |
| routing, rerank, temporal and kind routing on | 0.2909 | 0.6491 | 0.5822 | 0.4261 | 0.3996 |

Top1 and MRR improved and Recall@10 was flat, which is why those routing options became benchmark defaults.

### Release curated smoke (2026-03-24)

8 memories, 8 queries, lexical embeddings: Top1, Recall@5 and MRR are all 1.000 on both SQLite and pgvector. With 8 queries this checks that a backend works; it says nothing about quality. Source: `BENCHMARKS.MD`.

### Extractive F1 on LoCoMo

From a maintainer summary of earlier result sets (full-set rows dated 2026-03-13, 200-question rows 2026-03-06). The raw result files are not in this repository, so these rows cannot be re-derived from it.

| Configuration | Questions | F1 |
|---|---|---|
| Lexical + heuristic parser | 1533 | 12.9 |
| all-minilm via Ollama + heuristic parser | 1533 | 13.0 |
| Lexical, no LLM anywhere | 200 | 11.5 |
| Ollama parser at write time, extractive | 200 | 14.9 |
| Ollama parser at write time + qwen2.5:7b hybrid answer | 200 | 15.2 |
| Generate mode, qwen2.5:3b | 200 | 2.8 to 3.5 |

The embedding provider barely moves F1 (12.9 vs 13.0), so the bottleneck is recall of the right evidence, not the embedding or the importance scorer.

The 200-question lexical F1 of 11.5 (March) and 7.66 (v0.2.1, above) come from different samples, answer code and Pali versions; do not read the gap as a regression or an improvement.

## Reproduce

```bash
# no external services
bash test/benchmarks/profiles/release-curated-lexical.sh

# medium suite, lexical and Ollama/Qdrant
bash test/benchmarks/profiles/suite-medium-fast.sh
```

See [BENCHMARKS.MD](https://github.com/pali-mem/pali/blob/main/BENCHMARKS.MD) for every profile, suite and result layout, and the [research README](https://github.com/pali-mem/pali/blob/main/research/README.md) for the LoCoMo conversion and F1 scripts.
