# 2026-03-04: SQLite Write Throughput Tuning

## Summary

Adjusted SQLite connection pragmas in `internal/repository/sqlite/db.go` to improve write throughput for file-backed DBs:

- `PRAGMA journal_mode = WAL`
- `PRAGMA synchronous = NORMAL`
- `PRAGMA cache_size = -64000`
- `PRAGMA temp_store = MEMORY`

`PRAGMA foreign_keys = ON` remains enabled.

In-memory DSNs (`:memory:`, `file::memory:`, `mode=memory`) skip WAL/synchronous tuning.

## Why

The prior setup used SQLite defaults plus `foreign_keys`. That meant write-heavy benchmark runs paid higher transaction durability overhead per request, limiting store throughput.

## Benchmark Comparison

Workload (same for both runs):

- Backend: `sqlite`
- Fixture: `/tmp/pali-fixtures/memories_1000.json`
- Fixture count: `1000`
- Tenant count: `100`
- Search ops: `200`
- top_k: `10`
- Machine: `arm64 Darwin`

Baseline run (before pragma tuning):

- Timestamp: `2026-03-04T20:45:22Z`
- Artifact JSON: `test/benchmarks/results/20260304T204522Z_sqlite_memories_1000.json`
- Artifact summary: `test/benchmarks/results/20260304T204522Z_sqlite_memories_1000.summary.txt`

After-change run:

- Timestamp: `2026-03-04T20:52:29Z`
- Artifact JSON: `test/benchmarks/results/20260304T205229Z_sqlite_memories_1000.json`
- Artifact summary: `test/benchmarks/results/20260304T205229Z_sqlite_memories_1000.summary.txt`

Key metrics:

| Metric | Before | After | Delta |
|---|---:|---:|---:|
| Store throughput (ops/sec) | 44.284 | 54.294 | +22.60% |
| Store mean latency (ms) | 15.692 | 12.540 | -20.09% |
| Search throughput (ops/sec) | 34.926 | 29.219 | -16.34% |
| Search mean latency (ms) | 15.265 | 17.601 | +15.30% |

## Notes

- The tuning delivered a clear store/write improvement.
- Search is still dominated by full scan + JSON unmarshal + Go-side cosine scoring, and showed run-to-run variance in this single-sample comparison.
- This was a single before/after sample. For stronger confidence, we need to run multiple iterations and compare medians.

## Next Candidates

1. Add batch store path with explicit transactions to avoid per-row autocommit overhead.
2. Move embedding persistence from JSON text to binary blobs.
3. Replace full-scan vector search path for larger tenant datasets.
