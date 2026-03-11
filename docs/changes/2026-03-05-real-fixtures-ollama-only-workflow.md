# 2026-03-05 — Real Fixture Workflow (Ollama-only) + Retrieval Run

## What changed

- Removed template fixture-generation workflow and deprecated bulk template-set script:
  - deleted `scripts/gen_fixture_sets.sh`
  - removed `fixture-sets` target from `Makefile`
- Simplified fixture generation to one path (`cmd/genfix` -> Ollama only).
- Added parallel fixture generation to `cmd/genfix` via worker pool:
  - new `--parallel` flag
  - deterministic per-index RNG so seeded runs are reproducible even with concurrency
  - added HTTP timeout so stalled Ollama requests do not hang indefinitely
- Updated benchmark/retrieval scripts to use real embedding providers:
  - `scripts/benchmark.sh` and `scripts/retrieval_quality.sh` now support:
    - `--embedding-provider` (`ollama|onnx|mock`)
    - `--embedding-model` (Ollama model)
    - `--ollama-url`
    - `--onnx-model`
    - `--onnx-tokenizer`
  - default provider for script auto-start is now `ollama` (instead of `mock`)
  - added fast-fail Ollama readiness checks with clear setup guidance
- Updated docs and usage examples to match the new single-path workflow:
  - `README.md`
  - `BENCHMARKS.MD`
  - `scripts/bench_setup.sh` model list + commands

## Why

- Real-memory evaluation should use real model outputs and real embeddings.
- The old template path produced synthetic variants quickly, but it did not represent the intended operational path.
- Parallel generation is required for practical fixture build times.

## Benchmark evidence

### Real fixture generation

- command:
  - `scripts/gen_fixtures.sh --model phi4-mini --count 100 --tenants 20 --parallel 8 --seed 4242 --out test/fixtures/memories_real_phi4_100.json`
- result:
  - elapsed: `6m49.209s`
  - throughput: `0.2 items/s`
  - output: `test/fixtures/memories_real_phi4_100.json`

### Retrieval quality on real fixture

- command:
  - `./scripts/retrieval_quality.sh --fixture test/fixtures/memories_real_phi4_100.json --backend sqlite --top-k 10 --max-queries 100 --embedding-provider ollama --embedding-model all-minilm`
- summary:
  - Recall@10: `1.000000`
  - nDCG@10: `0.752146`
  - MRR: `0.669507`
  - HitRate@10: `1.000000`
  - MicroRecall@10: `1.000000`
- artifacts:
  - JSON: `test/benchmarks/results/20260305T031730Z_sqlite_memories_real_phi4_100_retrieval_quality.json`
  - summary: `test/benchmarks/results/20260305T031730Z_sqlite_memories_real_phi4_100_retrieval_quality.summary.txt`

### Merged real-fixture corpus + retrieval quality

- source files:
  - `test/fixtures/memories_real_phi4_100.json`
  - `test/fixtures/memories_real_phi4_300.json` (partial run salvage)
  - `test/fixtures/memories_real_phi4_300_v2.json` (partial run salvage)
- salvage and combine:
  - valid objects extracted: `338` (`100 + 226 + 12`)
  - duplicate content rows removed: `13`
  - final combined fixture: `test/fixtures/memories_real_phi4_combined.json` (`325` memories)
- retrieval command:
  - `./scripts/retrieval_quality.sh --fixture test/fixtures/memories_real_phi4_combined.json --backend sqlite --top-k 10 --max-queries 200 --embedding-provider ollama --embedding-model all-minilm`
- metrics (`top_k=10`, 200 query cases run):
  - Recall@10: `0.740000`
  - nDCG@10: `0.478032`
  - MRR: `0.397714`
  - HitRate@10: `0.740000`
  - MicroRecall@10: `0.740000`
- artifacts:
  - JSON: `test/benchmarks/results/20260305T032731Z_sqlite_memories_real_phi4_combined_retrieval_quality.json`
  - summary: `test/benchmarks/results/20260305T032731Z_sqlite_memories_real_phi4_combined_retrieval_quality.summary.txt`

## Notes and follow-up

- The current retrieval harness auto-generates eval queries from fixture content prefixes; this is useful for smoke/regression checks, but not sufficient as a robust semantic-quality benchmark.
- Next step is still a curated labeled eval corpus (`query -> expected_ids[]`) with stable domains and harder negatives.
