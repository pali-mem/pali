#!/usr/bin/env bash
# retrieval_trend.sh — run retrieval quality and append a trend record.
set -euo pipefail

cd "$(dirname "$0")/.."

HISTORY_FILE="test/benchmarks/trends/retrieval_quality_history.jsonl"
LABEL=""
PASSTHROUGH=()

usage() {
  cat <<'EOF'
Usage:
  scripts/retrieval_trend.sh [flags] [-- retrieval_quality_flags...]

Flags:
  --history-file <path>   JSONL file to append trend rows
  --label <text>          Optional run label (e.g. "after-search-filter-refactor")
  --help                  Show this help

Any unknown flags are forwarded to scripts/retrieval_quality.sh.

Example:
  scripts/retrieval_trend.sh \
    --label "curated-eval-baseline" \
    --fixture test/fixtures/memories.json \
    --eval-set test/fixtures/retrieval_eval.curated.json \
    --top-k 10 --max-queries 0 \
    --embedding-provider ollama --embedding-model all-minilm
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --history-file)
      HISTORY_FILE="$2"
      shift 2
      ;;
    --label)
      LABEL="$2"
      shift 2
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    --)
      shift
      while [[ $# -gt 0 ]]; do
        PASSTHROUGH+=("$1")
        shift
      done
      ;;
    *)
      PASSTHROUGH+=("$1")
      shift
      ;;
  esac
done

run_output="$(scripts/retrieval_quality.sh "${PASSTHROUGH[@]}")"
printf '%s\n' "$run_output"

result_json="$(
  printf '%s\n' "$run_output" \
    | sed -n 's/^[[:space:]]*JSON[[:space:]]*:[[:space:]]*//p' \
    | tail -n1
)"
if [[ -z "$result_json" ]]; then
  result_json="$(
    printf '%s\n' "$run_output" \
      | sed -n 's/^[[:space:]]*JSON result[[:space:]]*:[[:space:]]*//p' \
      | tail -n1
  )"
fi
if [[ -z "$result_json" || ! -f "$result_json" ]]; then
  echo "ERROR: could not locate retrieval result JSON path from retrieval_quality output"
  exit 1
fi

history_dir="$(dirname "$HISTORY_FILE")"
mkdir -p "$history_dir"

git_commit="$(git rev-parse --short HEAD 2>/dev/null || true)"
if [[ -z "$git_commit" ]]; then
  git_commit="unknown"
fi
if [[ -z "$LABEL" ]]; then
  LABEL="retrieval-quality"
fi
timestamp_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

record="$(
  jq -cn \
    --arg timestamp_utc "$timestamp_utc" \
    --arg git_commit "$git_commit" \
    --arg label "$LABEL" \
    --arg result_path "$result_json" \
    --slurpfile result "$result_json" \
    '{
      timestamp_utc: $timestamp_utc,
      git_commit: $git_commit,
      label: $label,
      result_json: $result_path,
      backend: $result[0].backend,
      fixture: $result[0].fixture,
      embedding_provider: $result[0].embedding_provider,
      embedding_model: $result[0].embedding_model,
      top_k: $result[0].top_k,
      eval_set: ($result[0].eval_set // ""),
      eval_cases: ($result[0].eval_success // 0),
      metrics: ($result[0].metrics // {})
    }'
)"

printf '%s\n' "$record" >> "$HISTORY_FILE"

echo ""
echo "==> Trend record appended"
echo "    History : $HISTORY_FILE"
echo "    Label   : $LABEL"
echo "    Commit  : $git_commit"
