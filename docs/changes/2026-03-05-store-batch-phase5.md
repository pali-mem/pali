# 2026-03-05 Store Throughput Phase 5 (Service + API Batch Path)

## Why

Parser-heavy and fixture-ingest workloads were bottlenecked by per-memory HTTP + embed + DB write overhead.  
Phase 5 adds a public batch store path and makes batch-capable internals the default optimization path without introducing new config flags.

## What changed

- Core service:
  - Added `StoreBatch(ctx, []StoreInput)` to memory service.
  - `Store()` now delegates to `StoreBatch()` with one item, so single-store callers keep behavior while sharing the optimized pipeline.
  - Non-parser batch path:
    - validates and resolves all inputs
    - batch-embeds when embedder supports it (falls back to per-item embed)
    - batch-writes repository when supported
    - batch-upserts vectors when supported
  - Parser inputs still use parser flow per turn (with within-turn embedding precompute already added in Phase 3).

- API:
  - Added `POST /v1/memory/batch`.
  - Existing `POST /v1/memory` remains unchanged.

- Storage/vector backends:
  - Added optional `MemoryBatchRepository` and `VectorBatchStore` extension interfaces.
  - Implemented `StoreBatch` in SQLite memory repository.
  - Implemented `UpsertBatch` in sqlite-vec store.

- Embedding:
  - Added optional `BatchEmbedder` extension.
  - Ollama embedder now uses `/api/embed` with array input for real batch embedding.
  - Mock and ONNX embedders implement batch via loop fallback.

- Harnesses:
  - `research/eval_locomo_f1_bleu.py` now auto-detects `/v1/memory/batch` for fixture ingestion and falls back to `/v1/memory`.
  - `scripts/benchmark.sh` now auto-detects `/v1/memory/batch`, uses batched ingestion when available, and reports `store.mode` (`batch` or `single`).

## Verification

- Full test suite passed:
  - `go test ./... -count=1`
- Added/updated tests for:
  - API batch route + auth behavior
  - client SDK batch call
  - service batch embedding / parser mixed path
  - SQLite repository batch store + rollback
  - sqlite-vec batch upsert

## Throughput evidence

Sanity benchmark run (mock embedder) after Phase 5:

- Command:
  - `scripts/benchmark.sh --fixture test/fixtures/memories.json --embedding-provider mock --search-ops 20 --top-k 10`
- Artifact summary:
  - `test/benchmarks/results/20260305T200809Z_sqlite_memories.summary.txt`
- Store section:
  - `Mode: batch`
  - `Operations: 100`
  - `Success/Fail: 100 / 0`
  - `Duration: 108.154 ms`
  - `Throughput: 924.608 ops/sec`

## Notes

- No config toggle was added for this optimization pass.
- Clients can continue using `/v1/memory`; high-volume workflows can explicitly call `/v1/memory/batch`.
- Benchmark/eval harnesses auto-detect and use batch by default when supported.

