#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../../.."

exec ./scripts/benchmark.sh \
  --fixture testdata/benchmarks/fixtures/release_memories.json \
  --eval-set testdata/benchmarks/evals/release_curated.json \
  --backend sqlite \
  --search-ops 200 \
  --top-k 5 \
  --embedding-provider lexical \
  "$@"
