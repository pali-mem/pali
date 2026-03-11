#!/usr/bin/env bash
# debug_locomo_query.sh — quick LOCOMO single-query debug without full benchmark cost.
set -euo pipefail

cd "$(dirname "$0")/.."

TENANT_ID="locomo_conv-26"
QUERY="When did Caroline go to the LGBTQ support group?"
FIXTURE_IN="research/data/locomo10.paperlite.fixture.json"
EVAL_IN="research/data/locomo10.paperlite.eval.json"
OUT_DIR="research/debug"
DB_PATH="research/debug/locomo_query.debug.sqlite"
INDEX_MAP_PATH="research/debug/locomo_query.debug.idx_map.json"
RUN_MODE="reuse" # rebuild|reuse
PORT="18088"
TOP_K="60"
EMBED_MODEL="all-minilm"

usage() {
  cat <<'EOF'
Usage:
  research/debug_locomo_query.sh [flags]

Flags:
  --tenant-id <id>       LOCOMO tenant id (default: locomo_conv-26)
  --query <text>         Exact query text to debug
  --fixture <path>       Input fixture JSON (default: research/data/locomo10.paperlite.fixture.json)
  --eval-set <path>      Input eval-set JSON (default: research/data/locomo10.paperlite.eval.json)
  --out-dir <path>       Output folder (default: research/debug)
  --db-path <path>       SQLite path for debug store (default: research/debug/locomo_query.debug.sqlite)
  --index-map <path>     Index map path (default: research/debug/locomo_query.debug.idx_map.json)
  --run-mode <mode>      rebuild | reuse (default: reuse)
  --port <n>             Eval server port (default: 18088)
  --top-k <n>            Retrieval top_k (default: 60)
  --embed-model <name>   Embedding model (default: all-minilm)
  --help                 Show help

Examples:
  research/debug_locomo_query.sh --run-mode rebuild
  research/debug_locomo_query.sh --tenant-id locomo_conv-26 --query "When did Caroline go to the LGBTQ support group?" --run-mode reuse
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tenant-id) TENANT_ID="$2"; shift 2 ;;
    --query) QUERY="$2"; shift 2 ;;
    --fixture) FIXTURE_IN="$2"; shift 2 ;;
    --eval-set) EVAL_IN="$2"; shift 2 ;;
    --out-dir) OUT_DIR="$2"; shift 2 ;;
    --db-path) DB_PATH="$2"; shift 2 ;;
    --index-map) INDEX_MAP_PATH="$2"; shift 2 ;;
    --run-mode) RUN_MODE="$2"; shift 2 ;;
    --port) PORT="$2"; shift 2 ;;
    --top-k) TOP_K="$2"; shift 2 ;;
    --embed-model) EMBED_MODEL="$2"; shift 2 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ERROR: unknown flag: $1"; usage; exit 1 ;;
  esac
done

if [[ "$RUN_MODE" != "rebuild" && "$RUN_MODE" != "reuse" ]]; then
  echo "ERROR: --run-mode must be rebuild or reuse"
  exit 1
fi

mkdir -p "$OUT_DIR"
subset_fixture="$OUT_DIR/${TENANT_ID}.fixture.json"
subset_eval="$OUT_DIR/${TENANT_ID}.eval.one.json"
trace_out="$OUT_DIR/${TENANT_ID}.trace.one.jsonl"
json_out="$OUT_DIR/${TENANT_ID}.one.json"
summary_out="$OUT_DIR/${TENANT_ID}.one.summary.txt"

jq --arg tenant "$TENANT_ID" '[.[] | select(.tenant_id == $tenant)]' "$FIXTURE_IN" > "$subset_fixture"
fixture_rows="$(jq 'length' "$subset_fixture")"
if [[ "$fixture_rows" -le 0 ]]; then
  echo "ERROR: no fixture rows found for tenant_id=$TENANT_ID in $FIXTURE_IN"
  exit 1
fi

jq --arg tenant "$TENANT_ID" --arg query "$QUERY" \
  '[.[] | select(.tenant_id == $tenant and .query == $query)]' \
  "$EVAL_IN" > "$subset_eval"
eval_rows="$(jq 'length' "$subset_eval")"
if [[ "$eval_rows" -ne 1 ]]; then
  echo "ERROR: expected exactly 1 eval row; got $eval_rows for tenant_id=$TENANT_ID query=$QUERY"
  exit 1
fi

cache_flags=(--db-path "$DB_PATH" --index-map-path "$INDEX_MAP_PATH")
if [[ "$RUN_MODE" == "rebuild" ]]; then
  cache_flags+=(--reset-db)
else
  cache_flags+=(--reuse-existing-store)
fi

echo "==> Debug query run"
echo "    tenant       : $TENANT_ID"
echo "    query        : $QUERY"
echo "    fixture rows : $fixture_rows"
echo "    run mode     : $RUN_MODE"
echo "    db path      : $DB_PATH"
echo ""

python3 research/eval_locomo_f1_bleu.py \
  --fixture "$subset_fixture" \
  --eval-set "$subset_eval" \
  --embedding-provider ollama \
  --embedding-model "$EMBED_MODEL" \
  --ollama-url http://127.0.0.1:11434 \
  --top-k "$TOP_K" \
  --max-queries 1 \
  --host 127.0.0.1 \
  --port "$PORT" \
  --server-start-timeout-seconds 240 \
  --answer-mode extractive \
  --extractive-confidence-threshold 0.42 \
  --prefer-extractive-for-temporal \
  --evidence-max-lines 10 \
  --retrieval-kind-routing \
  --structured-memory-enabled \
  --structured-query-routing-enabled \
  --structured-max-observations 4 \
  --parser-enabled \
  --parser-provider heuristic \
  --parser-store-raw-turn \
  --parser-max-facts 5 \
  --parser-dedupe-threshold 0.88 \
  --parser-update-threshold 0.94 \
  --trace-jsonl "$trace_out" \
  "${cache_flags[@]}" \
  --out-json "$json_out" \
  --out-summary "$summary_out"

echo ""
echo "==> Debug result (single query)"
jq -r '
  [
    "hit_ranks                : \(.hit_ranks)",
    "top1_text                : \(.top1_text)",
    "extractive_answer        : \(.extractive_answer)",
    "reference_answer         : \(.reference_answer)",
    "expected_memory_ids      : \(.expected_memory_ids)",
    "returned_ids_topk_first12: \(.returned_ids_topk)"
  ] | .[]' "$trace_out"

echo ""
echo "Artifacts:"
echo "  summary : $summary_out"
echo "  json    : $json_out"
echo "  trace   : $trace_out"
