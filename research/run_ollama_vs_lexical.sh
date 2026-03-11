#!/usr/bin/env bash
# run_ollama_vs_lexical.sh — paper-inspired retrieval study, without LLM-as-a-judge.
set -euo pipefail

cd "$(dirname "$0")/.."

FIXTURE="test/benchmarks/generated/memories_real_phi4_combined.json"
EVAL_SET=""
BACKEND="sqlite"
TOP_K=10
MAX_QUERIES=200
QUERY_WORDS=3
OLLAMA_URL="http://127.0.0.1:11434"
OLLAMA_MODEL="all-minilm"
OUT_ROOT="research/results"

usage() {
  cat <<'EOF'
Usage:
  research/run_ollama_vs_lexical.sh [flags]

Flags:
  --fixture <path>       Fixture JSON (default: test/benchmarks/generated/memories_real_phi4_combined.json)
  --eval-set <path>      Optional labeled eval set (recommended for stronger signal)
  --backend <name>       sqlite (default: sqlite)
  --top-k <n>            top_k for retrieval eval (default: 10)
  --max-queries <n>      Max eval queries (default: 200)
  --query-words <n>      Auto-query words if no eval-set (default: 3)
  --ollama-url <url>     Ollama base URL (default: http://127.0.0.1:11434)
  --ollama-model <name>  Ollama embedding model (default: all-minilm)
  --out-root <path>      Research output root (default: research/results)
  --help                 Show help

This runner:
  1) runs scripts/retrieval_quality.sh with embedding-provider=ollama
  2) runs scripts/retrieval_quality.sh with embedding-provider=lexical
  3) writes a benchmark-quality report (no LLM judge) in research/results/<run_id>/
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --fixture)
      FIXTURE="$2"
      shift 2
      ;;
    --eval-set)
      EVAL_SET="$2"
      shift 2
      ;;
    --backend)
      BACKEND="$2"
      shift 2
      ;;
    --top-k)
      TOP_K="$2"
      shift 2
      ;;
    --max-queries)
      MAX_QUERIES="$2"
      shift 2
      ;;
    --query-words)
      QUERY_WORDS="$2"
      shift 2
      ;;
    --ollama-url)
      OLLAMA_URL="$2"
      shift 2
      ;;
    --ollama-model)
      OLLAMA_MODEL="$2"
      shift 2
      ;;
    --out-root)
      OUT_ROOT="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "ERROR: unknown flag: $1"
      usage
      exit 1
      ;;
  esac
done

require_file() {
  local path="$1"
  if [[ ! -f "$path" ]]; then
    echo "ERROR: file not found: $path"
    exit 1
  fi
}

require_file "$FIXTURE"
if [[ -n "$EVAL_SET" ]]; then
  require_file "$EVAL_SET"
fi

run_id="$(date -u +%Y%m%dT%H%M%SZ)"
run_dir="${OUT_ROOT}/${run_id}"
raw_dir="${run_dir}/raw"

mkdir -p "$raw_dir"

run_provider() {
  local provider="$1"
  local latest_before latest_after
  local -a cmd

  latest_before="$(ls -1t "$raw_dir"/*_retrieval_quality.json 2>/dev/null | head -n1 || true)"

  cmd=(
    ./scripts/retrieval_quality.sh
    --fixture "$FIXTURE"
    --backend "$BACKEND"
    --out-dir "$raw_dir"
    --top-k "$TOP_K"
    --max-queries "$MAX_QUERIES"
    --query-words "$QUERY_WORDS"
    --embedding-provider "$provider"
  )

  if [[ -n "$EVAL_SET" ]]; then
    cmd+=(--eval-set "$EVAL_SET")
  fi

  if [[ "$provider" == "ollama" ]]; then
    cmd+=(--embedding-model "$OLLAMA_MODEL" --ollama-url "$OLLAMA_URL")
  fi

  echo "==> Running provider=${provider}"
  "${cmd[@]}"

  latest_after="$(ls -1t "$raw_dir"/*_retrieval_quality.json 2>/dev/null | head -n1 || true)"
  if [[ -z "$latest_after" || "$latest_after" == "$latest_before" ]]; then
    echo "ERROR: could not locate new retrieval_quality result for provider=${provider}"
    exit 1
  fi
  printf '%s' "$latest_after"
}

ollama_json="$(run_provider "ollama")"
echo ""
lexical_json="$(run_provider "lexical")"
echo ""

report_json="${run_dir}/benchmark_quality.json"
report_md="${run_dir}/README.md"

./research/analyze_benchmark_quality.sh \
  --ollama-json "$ollama_json" \
  --lexical-json "$lexical_json" \
  --out-json "$report_json" \
  --out-md "$report_md"

echo ""
echo "==> Research run complete"
echo "    run dir      : $run_dir"
echo "    ollama json  : $ollama_json"
echo "    lexical json : $lexical_json"
echo "    report json  : $report_json"
echo "    report md    : $report_md"
