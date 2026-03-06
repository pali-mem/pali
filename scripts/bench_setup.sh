#!/usr/bin/env bash
# bench_setup.sh — pull models required for current Pali benchmark workflows
set -euo pipefail

REQUIRED_MODELS=(
  "all-minilm"
  "phi4-mini"
)

echo "==> Pali benchmark setup"
echo ""

# Check ollama is available
if ! command -v ollama &>/dev/null; then
  echo "ERROR: ollama is not installed or not in PATH"
  echo "  Install from https://ollama.com"
  exit 1
fi

echo "--- Ollama models"
echo ""

PULLED=0
SKIPPED=0

for model in "${REQUIRED_MODELS[@]}"; do
  if ollama show "$model" &>/dev/null 2>&1; then
    echo "  [ok]     $model"
    ((SKIPPED++)) || true
  else
    echo "  [pull]   $model"
    ollama pull "$model"
    ((PULLED++)) || true
  fi
done

echo ""
echo "--- Summary"
echo "  Models already present : $SKIPPED"
echo "  Models pulled          : $PULLED"
echo ""

echo "--- Installed models"
ollama ls
echo ""

echo "==> Ready. Run benchmarks with:"
echo ""
echo "    scripts/benchmark.sh --fixture test/fixtures/memories.json --backend sqlite --embedding-provider ollama --embedding-model all-minilm"
echo ""
echo "    Or generate fresh fixtures first:"
echo "    scripts/gen_fixtures.sh --model phi4-mini --count 1000 --tenants 100 --parallel 8 --out test/fixtures/memories_real.json"
