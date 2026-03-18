#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/../../.."

exec python ./test/benchmarks/benchmark_suite.py \
  --config test/benchmarks/suites/speed.medium.qdrant-openrouter-parser-graph.json \
  "$@"
