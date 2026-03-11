#!/usr/bin/env bash
# gen_fixtures.sh — thin wrapper around cmd/genfix
#
# Usage:
#   scripts/gen_fixtures.sh --model phi4-mini --count 1000 --tenants 100 --parallel 8 --out test/benchmarks/generated/memories_real.json
#   scripts/gen_fixtures.sh --count 10 --parallel 4   # quick smoke test
set -euo pipefail

cd "$(dirname "$0")/.."

exec go run ./cmd/genfix "$@"
