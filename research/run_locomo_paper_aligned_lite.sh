#!/usr/bin/env bash
# run_locomo_paper_aligned_lite.sh — LOCOMO paper-aligned-lite QA metrics (F1/BLEU, no judge).
set -euo pipefail

cd "$(dirname "$0")/.."

LOCOMO_JSON="research/data/locomo10.json"
FIXTURE_OUT="research/data/locomo10.paperlite.fixture.json"
EVAL_OUT="research/data/locomo10.paperlite.eval.json"
STATS_OUT="research/data/locomo10.paperlite.stats.json"
NUM_CONVS=0
OUT_DIR="research/results/paperlite"
TOP_K=60
MAX_QUERIES=400
EMBED_MODEL="all-minilm"
ANSWER_MODEL="qwen2.5:7b"
ANSWER_MODE="hybrid"
ANSWER_TOP_DOCS=8
ANSWER_TIMEOUT=45
EVIDENCE_MAX_LINES=10
STRUCTURED_MAX_OBS=4
EXTRACTIVE_THRESHOLD=0.42
PARSER_PROVIDER="heuristic"
PARSER_STORE_RAW=true
PARSER_MAX_FACTS=5
PARSER_DEDUPE_THRESHOLD=0.88
PARSER_UPDATE_THRESHOLD=0.94
PARSER_OLLAMA_URL="http://127.0.0.1:11434"
PARSER_OLLAMA_MODEL="qwen2.5:7b"
PARSER_OLLAMA_TIMEOUT_MS=20000
STORE_BATCH_SIZE=64
STORE_BATCH_TIMEOUT_SECONDS=90
STORE_SINGLE_TIMEOUT_SECONDS=45
CACHE_DB="research/cache/paperlite_structured_ollama.sqlite"
CACHE_INDEX_MAP="research/cache/paperlite_structured_ollama_idx_map.json"
REUSE_CACHE=false
RESET_CACHE=false
SERVER_START_TIMEOUT=300

usage() {
  cat <<'EOF'
Usage:
  research/run_locomo_paper_aligned_lite.sh [flags]

Flags:
  --locomo-json <path>      LOCOMO source JSON (default: research/data/locomo10.json)
  --out-dir <path>          Output directory for run artifacts
  --top-k <n>               Retrieval top_k (default: 60)
  --max-queries <n>         Max eval queries (default: 400, -1 for all)
  --embed-model <name>      Embedding model for ollama provider (default: all-minilm)
  --answer-mode <mode>      Answer mode: extractive|generate|hybrid (default: hybrid)
  --answer-model <name>     Ollama model for answer generation (default: qwen2.5:7b)
  --answer-top-docs <n>     Number of retrieved docs passed to generator (default: 8)
  --answer-timeout <sec>    Generation timeout per query (default: 45)
  --evidence-max-lines <n>  Evidence lines passed to generator (default: 10)
  --structured-max-obs <n>  Max derived observations per turn (default: 4)
  --extractive-thr <v>      Extractive confidence threshold 0..1 (default: 0.42)
  --parser-provider <name>  Parser provider heuristic|ollama (default: heuristic)
  --parser-model <name>     Ollama model for parser (default: qwen2.5:7b)
  --parser-timeout-ms <n>   Ollama parser timeout ms (default: 20000)
  --store-batch-size <n>    Ingest batch size for /v1/memory/batch (default: 64)
  --store-batch-timeout <s> Timeout per batch ingest request in seconds (default: 90)
  --store-single-timeout <s> Timeout per single ingest request in seconds (default: 45)
  --parser-store-raw        Store raw turns with parser enabled (default: on)
  --parser-no-store-raw     Disable raw-turn storage when parser is enabled
  --parser-max-facts <n>    Max parser facts per turn (default: 5)
  --cache-db <path>         Persistent ollama cache DB path
  --cache-index-map <path>  Persistent fixture-index map path
  --reuse-cache             Reuse existing --cache-db + --cache-index-map for ollama run
  --reset-cache             Delete existing --cache-db before ollama run
  --num-convs <n>           Limit to first N conversations for fast dev-loop (0 = all 10, default: 0)
                            e.g. --num-convs 3 → ~1800 rows, ~45 min parse, ~36 queries
  --server-start-timeout <s> Server startup timeout seconds (default: 300)
  --help                    Show help

Outputs:
  - ollama (hybrid) QA metrics JSON/summary
  - lexical QA metrics JSON/summary
  - comparison JSON (ollama - lexical deltas)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --locomo-json)
      LOCOMO_JSON="$2"; shift 2 ;;
    --out-dir)
      OUT_DIR="$2"; shift 2 ;;
    --top-k)
      TOP_K="$2"; shift 2 ;;
    --max-queries)
      MAX_QUERIES="$2"; shift 2 ;;
    --embed-model)
      EMBED_MODEL="$2"; shift 2 ;;
    --answer-mode)
      ANSWER_MODE="$2"; shift 2 ;;
    --answer-model)
      ANSWER_MODEL="$2"; shift 2 ;;
    --answer-top-docs)
      ANSWER_TOP_DOCS="$2"; shift 2 ;;
    --answer-timeout)
      ANSWER_TIMEOUT="$2"; shift 2 ;;
    --evidence-max-lines)
      EVIDENCE_MAX_LINES="$2"; shift 2 ;;
    --structured-max-obs)
      STRUCTURED_MAX_OBS="$2"; shift 2 ;;
    --extractive-thr)
      EXTRACTIVE_THRESHOLD="$2"; shift 2 ;;
    --parser-provider)
      PARSER_PROVIDER="$2"; shift 2 ;;
    --parser-store-raw)
      PARSER_STORE_RAW=true; shift ;;
    --parser-no-store-raw)
      PARSER_STORE_RAW=false; shift ;;
    --parser-model)
      PARSER_OLLAMA_MODEL="$2"; shift 2 ;;
    --parser-timeout-ms)
      PARSER_OLLAMA_TIMEOUT_MS="$2"; shift 2 ;;
    --store-batch-size)
      STORE_BATCH_SIZE="$2"; shift 2 ;;
    --store-batch-timeout)
      STORE_BATCH_TIMEOUT_SECONDS="$2"; shift 2 ;;
    --store-single-timeout)
      STORE_SINGLE_TIMEOUT_SECONDS="$2"; shift 2 ;;
    --parser-max-facts)
      PARSER_MAX_FACTS="$2"; shift 2 ;;
    --cache-db)
      CACHE_DB="$2"; shift 2 ;;
    --cache-index-map)
      CACHE_INDEX_MAP="$2"; shift 2 ;;
    --reuse-cache)
      REUSE_CACHE=true; shift ;;
    --reset-cache)
      RESET_CACHE=true; shift ;;
    --num-convs)
      NUM_CONVS="$2"; shift 2 ;;
    --server-start-timeout)
      SERVER_START_TIMEOUT="$2"; shift 2 ;;
    --help|-h)
      usage; exit 0 ;;
    *)
      echo "ERROR: unknown flag $1"
      usage
      exit 1 ;;
  esac
done

if [[ "$REUSE_CACHE" == "true" && "$RESET_CACHE" == "true" ]]; then
  echo "ERROR: --reuse-cache and --reset-cache are mutually exclusive"
  exit 1
fi

# Parser-LLM ingest is significantly heavier than heuristic parsing.
# If the caller keeps defaults, auto-tune store request shape to avoid batch timeouts.
if [[ "$PARSER_PROVIDER" == "ollama" ]]; then
  if [[ "$STORE_BATCH_SIZE" == "64" ]]; then
    STORE_BATCH_SIZE=8
  fi
  if [[ "$STORE_BATCH_TIMEOUT_SECONDS" == "90" ]]; then
    STORE_BATCH_TIMEOUT_SECONDS=600
  fi
  if [[ "$STORE_SINGLE_TIMEOUT_SECONDS" == "45" ]]; then
    STORE_SINGLE_TIMEOUT_SECONDS=120
  fi
fi

# Kill any stale pali server processes from previous interrupted runs.
pkill -f "go run ./cmd/pali" 2>/dev/null || true
pkill -f "pali -config" 2>/dev/null || true
sleep 0.5

mkdir -p "$(dirname "$LOCOMO_JSON")" "$OUT_DIR"
mkdir -p "$(dirname "$CACHE_DB")" "$(dirname "$CACHE_INDEX_MAP")"
if [[ ! -f "$LOCOMO_JSON" ]]; then
  echo "==> locomo10.json not found — downloading from snap-research/locomo..."
  curl -fsSL https://raw.githubusercontent.com/snap-research/locomo/main/data/locomo10.json \
    -o "$LOCOMO_JSON" || { echo "ERROR: download failed"; exit 1; }
  echo "    saved to $LOCOMO_JSON"
fi

# When mini mode is active, use separate fixture/eval paths so they don't clobber full-set files.
if [[ "$NUM_CONVS" -gt 0 ]]; then
  FIXTURE_OUT="research/data/locomo10.paperlite.mini${NUM_CONVS}.fixture.json"
  EVAL_OUT="research/data/locomo10.paperlite.mini${NUM_CONVS}.eval.json"
  STATS_OUT="research/data/locomo10.paperlite.mini${NUM_CONVS}.stats.json"
  echo "==> [mini] Using first ${NUM_CONVS} conversations"
fi

echo "==> Converting LOCOMO to paperlite fixture/eval"
_prep_extra_args=()
[[ "$NUM_CONVS" -gt 0 ]] && _prep_extra_args+=(--max-conversations "$NUM_CONVS")
python3 research/prepare_locomo_eval.py \
  --locomo-json "$LOCOMO_JSON" \
  --fixture-out "$FIXTURE_OUT" \
  --eval-out "$EVAL_OUT" \
  --stats-out "$STATS_OUT" \
  --mode paperlite \
  --sanitize-percent \
  "${_prep_extra_args[@]}"

run_id="$(date -u +%Y%m%dT%H%M%SZ)"
run_dir="$OUT_DIR/$run_id"
mkdir -p "$run_dir"

ollama_json="$run_dir/ollama.json"
ollama_txt="$run_dir/ollama.summary.txt"
lexical_json="$run_dir/lexical.json"
lexical_txt="$run_dir/lexical.summary.txt"
compare_json="$run_dir/comparison.json"
ollama_trace="$run_dir/ollama.trace.jsonl"
lexical_trace="$run_dir/lexical.trace.jsonl"

ollama_cache_flags=(--db-path "$CACHE_DB" --index-map-path "$CACHE_INDEX_MAP")
if [[ "$REUSE_CACHE" == "true" ]]; then
  ollama_cache_flags+=(--reuse-existing-store)
else
  # Safe default: non-reuse runs must not append into an existing cache DB.
  ollama_cache_flags+=(--reset-db)
fi
if [[ "$RESET_CACHE" == "true" ]]; then
  # Delete both the sqlite DB and the index map so the next run starts clean.
  # --reset-db alone only deletes the DB; a stale index map causes ID mismatches.
  if [[ -f "$CACHE_DB" ]]; then
    rm -f "$CACHE_DB"
    rm -f "$CACHE_DB-shm" "$CACHE_DB-wal"
    echo "reset-cache: removed $CACHE_DB"
  fi
  if [[ -f "$CACHE_INDEX_MAP" ]]; then
    rm -f "$CACHE_INDEX_MAP"
    echo "reset-cache: removed $CACHE_INDEX_MAP"
  fi
fi

parser_flags=(
  --parser-enabled
  --parser-provider "$PARSER_PROVIDER"
  --parser-max-facts "$PARSER_MAX_FACTS"
  --parser-dedupe-threshold "$PARSER_DEDUPE_THRESHOLD"
  --parser-update-threshold "$PARSER_UPDATE_THRESHOLD"
  --parser-ollama-url "$PARSER_OLLAMA_URL"
  --parser-ollama-model "$PARSER_OLLAMA_MODEL"
  --parser-ollama-timeout-ms "$PARSER_OLLAMA_TIMEOUT_MS"
)
if [[ "$PARSER_STORE_RAW" == "true" ]]; then
  parser_flags+=(--parser-store-raw-turn)
else
  parser_flags+=(--no-parser-store-raw-turn)
fi

echo "==> Store ingest settings"
echo "    batch size    : $STORE_BATCH_SIZE"
echo "    batch timeout : ${STORE_BATCH_TIMEOUT_SECONDS}s"
echo "    single timeout: ${STORE_SINGLE_TIMEOUT_SECONDS}s"

echo "==> Running hybrid (embedding-provider=ollama)"
python3 research/eval_locomo_f1_bleu.py \
  --fixture "$FIXTURE_OUT" \
  --eval-set "$EVAL_OUT" \
  --embedding-provider ollama \
  --embedding-model "$EMBED_MODEL" \
  --ollama-url http://127.0.0.1:11434 \
  --top-k "$TOP_K" \
  --max-queries "$MAX_QUERIES" \
  --host 127.0.0.1 \
  --port 18086 \
  --server-start-timeout-seconds "$SERVER_START_TIMEOUT" \
  --answer-mode "$ANSWER_MODE" \
  --answer-model "$ANSWER_MODEL" \
  --answer-top-docs "$ANSWER_TOP_DOCS" \
  --answer-ollama-url http://127.0.0.1:11434 \
  --answer-timeout-seconds "$ANSWER_TIMEOUT" \
  --extractive-confidence-threshold "$EXTRACTIVE_THRESHOLD" \
  --prefer-extractive-for-temporal \
  --evidence-max-lines "$EVIDENCE_MAX_LINES" \
  --structured-memory-enabled \
  --structured-query-routing-enabled \
  --structured-max-observations "$STRUCTURED_MAX_OBS" \
  --store-batch-size "$STORE_BATCH_SIZE" \
  --store-batch-timeout-seconds "$STORE_BATCH_TIMEOUT_SECONDS" \
  --store-single-timeout-seconds "$STORE_SINGLE_TIMEOUT_SECONDS" \
  "${parser_flags[@]}" \
  --trace-jsonl "$ollama_trace" \
  "${ollama_cache_flags[@]}" \
  --out-json "$ollama_json" \
  --out-summary "$ollama_txt"

echo "==> Running lexical-only"
python3 research/eval_locomo_f1_bleu.py \
  --fixture "$FIXTURE_OUT" \
  --eval-set "$EVAL_OUT" \
  --embedding-provider lexical \
  --embedding-model "$EMBED_MODEL" \
  --top-k "$TOP_K" \
  --max-queries "$MAX_QUERIES" \
  --host 127.0.0.1 \
  --port 18087 \
  --server-start-timeout-seconds "$SERVER_START_TIMEOUT" \
  --answer-mode "$ANSWER_MODE" \
  --answer-model "$ANSWER_MODEL" \
  --answer-top-docs "$ANSWER_TOP_DOCS" \
  --answer-ollama-url http://127.0.0.1:11434 \
  --answer-timeout-seconds "$ANSWER_TIMEOUT" \
  --extractive-confidence-threshold "$EXTRACTIVE_THRESHOLD" \
  --prefer-extractive-for-temporal \
  --evidence-max-lines "$EVIDENCE_MAX_LINES" \
  --structured-memory-enabled \
  --structured-query-routing-enabled \
  --structured-max-observations "$STRUCTURED_MAX_OBS" \
  --store-batch-size "$STORE_BATCH_SIZE" \
  --store-batch-timeout-seconds "$STORE_BATCH_TIMEOUT_SECONDS" \
  --store-single-timeout-seconds "$STORE_SINGLE_TIMEOUT_SECONDS" \
  "${parser_flags[@]}" \
  --trace-jsonl "$lexical_trace" \
  --out-json "$lexical_json" \
  --out-summary "$lexical_txt"

jq -n \
  --slurpfile o "$ollama_json" \
  --slurpfile l "$lexical_json" \
  '{
    ollama: {
      f1_generated: $o[0].qa_metrics.f1_generated,
      bleu1_generated: $o[0].qa_metrics.bleu1_generated,
      f1_generated_paper_scale: $o[0].qa_metrics_paper_scale.f1_generated,
      bleu1_generated_paper_scale: $o[0].qa_metrics_paper_scale.bleu1_generated,
      retrieval_recall_at_k: $o[0].retrieval_metrics.recall_at_k,
      retrieval_ndcg_at_k: $o[0].retrieval_metrics.ndcg_at_k,
      retrieval_mrr: $o[0].retrieval_metrics.mrr,
      em_generated_normalized: $o[0].qa_metrics_companion.em_generated_normalized,
      top1_unique_rate: $o[0].retrieval_diagnostics.top1_unique_rate
    },
    lexical: {
      f1_generated: $l[0].qa_metrics.f1_generated,
      bleu1_generated: $l[0].qa_metrics.bleu1_generated,
      f1_generated_paper_scale: $l[0].qa_metrics_paper_scale.f1_generated,
      bleu1_generated_paper_scale: $l[0].qa_metrics_paper_scale.bleu1_generated,
      retrieval_recall_at_k: $l[0].retrieval_metrics.recall_at_k,
      retrieval_ndcg_at_k: $l[0].retrieval_metrics.ndcg_at_k,
      retrieval_mrr: $l[0].retrieval_metrics.mrr,
      em_generated_normalized: $l[0].qa_metrics_companion.em_generated_normalized,
      top1_unique_rate: $l[0].retrieval_diagnostics.top1_unique_rate
    },
    delta_ollama_minus_lexical: {
      f1_generated: ($o[0].qa_metrics.f1_generated - $l[0].qa_metrics.f1_generated),
      bleu1_generated: ($o[0].qa_metrics.bleu1_generated - $l[0].qa_metrics.bleu1_generated),
      f1_generated_paper_scale: ($o[0].qa_metrics_paper_scale.f1_generated - $l[0].qa_metrics_paper_scale.f1_generated),
      bleu1_generated_paper_scale: ($o[0].qa_metrics_paper_scale.bleu1_generated - $l[0].qa_metrics_paper_scale.bleu1_generated),
      retrieval_recall_at_k: ($o[0].retrieval_metrics.recall_at_k - $l[0].retrieval_metrics.recall_at_k),
      retrieval_ndcg_at_k: ($o[0].retrieval_metrics.ndcg_at_k - $l[0].retrieval_metrics.ndcg_at_k),
      retrieval_mrr: ($o[0].retrieval_metrics.mrr - $l[0].retrieval_metrics.mrr),
      em_generated_normalized: ($o[0].qa_metrics_companion.em_generated_normalized - $l[0].qa_metrics_companion.em_generated_normalized),
      top1_unique_rate: ($o[0].retrieval_diagnostics.top1_unique_rate - $l[0].retrieval_diagnostics.top1_unique_rate)
    }
  }' > "$compare_json"

echo ""
echo "==> Paper-aligned-lite run complete"
echo "    run dir      : $run_dir"
echo "    ollama json  : $ollama_json"
echo "    ollama trace : $ollama_trace"
echo "    lexical json : $lexical_json"
echo "    lexical trace: $lexical_trace"
echo "    cache db     : $CACHE_DB"
echo "    cache idxmap : $CACHE_INDEX_MAP"
echo "    comparison   : $compare_json"
