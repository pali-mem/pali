#!/usr/bin/env bash
# run_locomo_paper_style.sh — LOCOMO-based ollama vs lexical retrieval comparison.
set -euo pipefail

cd "$(dirname "$0")/.."

LOCOMO_JSON="research/data/locomo10.json"
FIXTURE_OUT="research/data/locomo10.fixture.json"
EVAL_OUT="research/data/locomo10.eval.json"
STATS_OUT="research/data/locomo10.stats.json"
MAX_QUERIES="-1"
TOP_K="10"
QUERY_WORDS="3"
OLLAMA_MODEL="all-minilm"
OLLAMA_URL="http://127.0.0.1:11434"
OUT_ROOT="research/results"
DOWNLOAD_IF_MISSING="1"

usage() {
  cat <<'EOF'
Usage:
  research/run_locomo_paper_style.sh [flags]

Flags:
  --locomo-json <path>       LOCOMO JSON path (default: research/data/locomo10.json)
  --fixture-out <path>       Converted fixture output path
  --eval-out <path>          Converted eval output path
  --stats-out <path>         Conversion stats output path
  --max-queries <n>          Max eval queries passed to retrieval harness (-1 = all, default)
  --top-k <n>                Search top_k (default: 10)
  --query-words <n>          Unused for curated eval, passed through for completeness
  --ollama-model <name>      Ollama embedding model (default: all-minilm)
  --ollama-url <url>         Ollama URL (default: http://127.0.0.1:11434)
  --out-root <path>          Research results root (default: research/results)
  --no-download              Do not auto-download LOCOMO JSON if missing
  --help                     Show help

This workflow:
  1) downloads LOCOMO sample (locomo10.json) if missing,
  2) converts it to Pali fixture + eval-set with evidence labels,
  3) runs ollama vs lexical via existing retrieval benchmark script,
  4) writes benchmark-quality report without LLM judge.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --locomo-json)
      LOCOMO_JSON="$2"
      shift 2
      ;;
    --fixture-out)
      FIXTURE_OUT="$2"
      shift 2
      ;;
    --eval-out)
      EVAL_OUT="$2"
      shift 2
      ;;
    --stats-out)
      STATS_OUT="$2"
      shift 2
      ;;
    --max-queries)
      MAX_QUERIES="$2"
      shift 2
      ;;
    --top-k)
      TOP_K="$2"
      shift 2
      ;;
    --query-words)
      QUERY_WORDS="$2"
      shift 2
      ;;
    --ollama-model)
      OLLAMA_MODEL="$2"
      shift 2
      ;;
    --ollama-url)
      OLLAMA_URL="$2"
      shift 2
      ;;
    --out-root)
      OUT_ROOT="$2"
      shift 2
      ;;
    --no-download)
      DOWNLOAD_IF_MISSING="0"
      shift 1
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

mkdir -p "$(dirname "$LOCOMO_JSON")"

if [[ ! -f "$LOCOMO_JSON" ]]; then
  if [[ "$DOWNLOAD_IF_MISSING" == "1" ]]; then
    echo "==> Downloading LOCOMO sample dataset"
    curl -sL https://raw.githubusercontent.com/snap-research/locomo/main/data/locomo10.json -o "$LOCOMO_JSON"
  else
    echo "ERROR: LOCOMO file missing and --no-download set: $LOCOMO_JSON"
    exit 1
  fi
fi

echo "==> Converting LOCOMO to Pali fixture/eval format"
python3 research/prepare_locomo_eval.py \
  --locomo-json "$LOCOMO_JSON" \
  --fixture-out "$FIXTURE_OUT" \
  --eval-out "$EVAL_OUT" \
  --stats-out "$STATS_OUT"

echo "==> Running LOCOMO comparison (ollama vs lexical)"
research/run_ollama_vs_lexical.sh \
  --fixture "$FIXTURE_OUT" \
  --eval-set "$EVAL_OUT" \
  --top-k "$TOP_K" \
  --max-queries "$MAX_QUERIES" \
  --query-words "$QUERY_WORDS" \
  --ollama-model "$OLLAMA_MODEL" \
  --ollama-url "$OLLAMA_URL" \
  --out-root "$OUT_ROOT"

echo ""
echo "==> LOCOMO paper-style run complete"
echo "    locomo json : $LOCOMO_JSON"
echo "    fixture     : $FIXTURE_OUT"
echo "    eval set    : $EVAL_OUT"
echo "    stats       : $STATS_OUT"
