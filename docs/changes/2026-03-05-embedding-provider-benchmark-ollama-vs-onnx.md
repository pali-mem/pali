# 2026-03-05: Embedding Provider Benchmark (Ollama vs ONNX)

## Summary

Ran matched benchmark and retrieval-quality suites for two embedding providers:

- `embedding.provider=ollama` with model `all-minilm`
- `embedding.provider=onnx` with model `all-MiniLM-L6-v2`

Same workload for both runs:

- fixture: `test/fixtures/memories.json` (100 memories, 3 tenants)
- benchmark script: `scripts/benchmark.sh` (`search_ops=200`, `top_k=10`)
- retrieval script: `scripts/retrieval_quality.sh` with `test/fixtures/retrieval_eval.sample.json`
- machine: `arm64 Darwin`

## Results

### API benchmark

| Metric | Ollama | ONNX | Delta (ONNX vs Ollama) |
|---|---:|---:|---:|
| Store throughput (ops/sec) | 18.868 | 29.850 | +58.20% |
| Store mean latency (ms) | 46.309 | 26.346 | -43.11% |
| Search throughput (ops/sec) | 19.940 | 23.196 | +16.33% |
| Search mean latency (ms) | 36.043 | 29.194 | -19.00% |

### Retrieval quality (`top_k=10`)

| Metric | Ollama | ONNX |
|---|---:|---:|
| Recall@10 | 1.000000 | 1.000000 |
| nDCG@10 | 0.643559 | 0.643559 |
| MRR | 0.527778 | 0.527778 |
| HitRate@10 | 1.000000 | 1.000000 |

## Artifacts

Ollama:

- `test/benchmarks/results/20260305T023936Z_sqlite_memories.json`
- `test/benchmarks/results/20260305T023936Z_sqlite_memories.summary.txt`
- `test/benchmarks/results/20260305T023952Z_sqlite_memories_retrieval_quality.json`
- `test/benchmarks/results/20260305T023952Z_sqlite_memories_retrieval_quality.summary.txt`

ONNX:

- `test/benchmarks/results/20260305T024027Z_sqlite_memories.json`
- `test/benchmarks/results/20260305T024027Z_sqlite_memories.summary.txt`
- `test/benchmarks/results/20260305T024039Z_sqlite_memories_retrieval_quality.json`
- `test/benchmarks/results/20260305T024039Z_sqlite_memories_retrieval_quality.summary.txt`

## Notes

- ONNX was faster in this workload for both store and search latency/throughput.
- Retrieval-quality metrics were identical on the current labeled sample set.
- The sample eval set has only 3 queries; scale this up before treating quality parity as definitive.
