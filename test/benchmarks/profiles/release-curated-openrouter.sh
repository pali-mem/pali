#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../../.."

exec ./scripts/retrieval_quality.sh \
  --fixture testdata/benchmarks/fixtures/release_memories.json \
  --eval-set testdata/benchmarks/evals/release_curated.json \
  --backend sqlite \
  --top-k 5 \
  --max-queries 0 \
  --embedding-provider openrouter \
  "$@"
