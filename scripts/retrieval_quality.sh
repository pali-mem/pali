#!/usr/bin/env bash
# retrieval_quality.sh â€” offline retrieval quality evaluation using /v1/memory/search.
set -euo pipefail

cd "$(dirname "$0")/.."

FIXTURE="testdata/benchmarks/fixtures/release_memories.json"
EVAL_SET="testdata/benchmarks/evals/release_curated.json"
BACKEND="sqlite"
OUT_DIR="test/benchmarks/results"
TOP_K=5
MAX_QUERIES=200
QUERY_WORDS=3
SAMPLE_SEED=42
HOST="127.0.0.1"
PORT="18080"
BASE_URL=""
START_SERVER=1
EMBEDDING_PROVIDER="ollama"
ENTITY_FACT_BACKEND=""
CONFIG_PROFILE=""
OLLAMA_BASE_URL="http://127.0.0.1:11434"
OLLAMA_MODEL="all-minilm"
OLLAMA_TIMEOUT_SECONDS=10
ONNX_MODEL_PATH="./models/all-MiniLM-L6-v2/model.onnx"
ONNX_TOKENIZER_PATH="./models/all-MiniLM-L6-v2/tokenizer.json"
QDRANT_BASE_URL="http://127.0.0.1:6333"
QDRANT_API_KEY=""
QDRANT_COLLECTION="pali_memories"
QDRANT_TIMEOUT_MS=2000
QDRANT_ISOLATE_RUN="auto"
PARSER_ENABLED=""
PARSER_PROVIDER=""
PARSER_OPENROUTER_MODEL=""
STORE_CHUNK_SIZE=50
EVAL_TARGET_MODE="auto"

usage() {
  cat <<'EOF'
Usage:
  scripts/retrieval_quality.sh [flags]

Flags:
  --fixture <path>         Fixture JSON file used to store memories first (default: testdata/benchmarks/fixtures/release_memories.json)
  --eval-set <path>        Optional labeled eval set JSON (query + expected ids/indexes, default: testdata/benchmarks/evals/release_curated.json)
  --backend <name>         sqlite | qdrant (default: sqlite)
  --out-dir <path>         Output directory for JSON + summary results
  --top-k <n>              top_k used in search requests (default: 5)
  --max-queries <n>        Max number of eval queries to run (default: 200, <=0 means all)
  --query-words <n>        Auto-query first N words when --eval-set is not provided (default: 3)
  --sample-seed <n>        Deterministic sample seed when max-queries selects a subset (default: 42)
  --host <ip>              Server host for auto-start mode (default: 127.0.0.1)
  --port <port>            Server port for auto-start mode (default: 18080)
  --base-url <url>         Use an already-running server, disables auto-start
  --embedding-provider <p> ollama | onnx | lexical | mock | openrouter (default: ollama)
  --entity-fact-backend <b> sqlite | neo4j (default: from selected profile)
  --config-profile <path>  Base provider profile YAML (default: auto from provider/backend)
  --embedding-model <name> Ollama model name (default: all-minilm)
  --ollama-url <url>       Ollama base URL (default: http://127.0.0.1:11434)
  --onnx-model <path>      ONNX model path (default: ./models/all-MiniLM-L6-v2/model.onnx)
  --onnx-tokenizer <path>  ONNX tokenizer path (default: ./models/all-MiniLM-L6-v2/tokenizer.json)
  --qdrant-url <url>       Qdrant base URL (default: http://127.0.0.1:6333)
  --qdrant-api-key <key>   Qdrant API key (default: empty)
  --qdrant-collection <n>  Qdrant collection name (default: pali_memories)
  --qdrant-timeout-ms <n>  Qdrant request timeout (default: 2000)
  --qdrant-isolate-run <m> auto | true | false (default: auto; auto isolates when script starts server)
  --parser-enabled <bool>  Force parser on/off (true|false). Overrides neo4j auto-mode.
  --parser-provider <name> heuristic | ollama | openrouter (default: auto/heuristic for neo4j)
  --parser-openrouter-model <name> Override parser.openrouter_model for parser provider openrouter
  --store-chunk-size <n>   Memories per /v1/memory/batch call during fixture load (default: 50)
  --eval-target <mode>     auto | raw_turn | source_family | canonical (default: auto)
  --help                   Show this help

Eval set format (JSON array):
[
  {
    "tenant_id": "bench_tenant_001",
    "query": "dark mode preferences",
    "expected_fixture_indexes": [0, 5]
  },
  {
    "tenant_id": "bench_tenant_002",
    "query": "travel planning",
    "expected_memory_ids": ["mem_abc", "mem_xyz"]
  }
]
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
    --out-dir)
      OUT_DIR="$2"
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
    --sample-seed)
      SAMPLE_SEED="$2"
      shift 2
      ;;
    --host)
      HOST="$2"
      shift 2
      ;;
    --port)
      PORT="$2"
      shift 2
      ;;
    --base-url)
      BASE_URL="$2"
      START_SERVER=0
      shift 2
      ;;
    --embedding-provider)
      EMBEDDING_PROVIDER="$2"
      shift 2
      ;;
    --entity-fact-backend)
      ENTITY_FACT_BACKEND="$2"
      shift 2
      ;;
    --config-profile)
      CONFIG_PROFILE="$2"
      shift 2
      ;;
    --embedding-model)
      OLLAMA_MODEL="$2"
      shift 2
      ;;
    --ollama-url)
      OLLAMA_BASE_URL="$2"
      shift 2
      ;;
    --onnx-model)
      ONNX_MODEL_PATH="$2"
      shift 2
      ;;
    --onnx-tokenizer)
      ONNX_TOKENIZER_PATH="$2"
      shift 2
      ;;
    --qdrant-url)
      QDRANT_BASE_URL="$2"
      shift 2
      ;;
    --qdrant-api-key)
      QDRANT_API_KEY="$2"
      shift 2
      ;;
    --qdrant-collection)
      QDRANT_COLLECTION="$2"
      shift 2
      ;;
    --qdrant-timeout-ms)
      QDRANT_TIMEOUT_MS="$2"
      shift 2
      ;;
    --qdrant-isolate-run)
      QDRANT_ISOLATE_RUN="$2"
      shift 2
      ;;
    --parser-enabled)
      PARSER_ENABLED="$2"
      shift 2
      ;;
    --parser-provider)
      PARSER_PROVIDER="$2"
      shift 2
      ;;
    --parser-openrouter-model)
      PARSER_OPENROUTER_MODEL="$2"
      shift 2
      ;;
    --store-chunk-size)
      STORE_CHUNK_SIZE="$2"
      shift 2
      ;;
    --eval-target)
      EVAL_TARGET_MODE="$2"
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

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "ERROR: required command not found: $1"
    exit 1
  fi
}

require_cmd curl
require_cmd jq
require_cmd awk
require_cmd sort
require_cmd head
require_cmd cut

if [[ ! -f "$FIXTURE" ]]; then
  echo "ERROR: fixture file not found: $FIXTURE"
  exit 1
fi

if [[ -n "$EVAL_SET" && ! -f "$EVAL_SET" ]]; then
  echo "ERROR: eval set file not found: $EVAL_SET"
  exit 1
fi

case "$BACKEND" in
  sqlite|qdrant)
    ;;
  *)
    echo "ERROR: --backend must be one of: sqlite, qdrant"
    exit 1
    ;;
esac

case "$EMBEDDING_PROVIDER" in
  ollama|onnx|mock|lexical|openrouter)
    ;;
  *)
    echo "ERROR: --embedding-provider must be one of: ollama, onnx, mock, lexical, openrouter"
    exit 1
    ;;
esac

case "$QDRANT_ISOLATE_RUN" in
  auto|true|false)
    ;;
  *)
    echo "ERROR: --qdrant-isolate-run must be one of: auto, true, false"
    exit 1
    ;;
esac

case "$EVAL_TARGET_MODE" in
  auto|raw_turn|source_family|canonical)
    ;;
  *)
    echo "ERROR: --eval-target must be one of: auto, raw_turn, source_family, canonical"
    exit 1
    ;;
esac

# Avoid cross-run vector-size collisions in shared Qdrant collections.
if [[ "$EMBEDDING_PROVIDER" == "lexical" && "$QDRANT_COLLECTION" == "pali_memories" ]]; then
  QDRANT_COLLECTION="pali_memories_lexical"
fi

if [[ -n "$ENTITY_FACT_BACKEND" ]]; then
  case "$ENTITY_FACT_BACKEND" in
    sqlite|neo4j)
      ;;
    *)
      echo "ERROR: --entity-fact-backend must be one of: sqlite, neo4j"
      exit 1
      ;;
  esac
fi

PARSER_ENABLED_OVERRIDE=""
PARSER_PROVIDER_OVERRIDE=""
if [[ "$ENTITY_FACT_BACKEND" == "neo4j" ]]; then
  PARSER_ENABLED_OVERRIDE="true"
  PARSER_PROVIDER_OVERRIDE="heuristic"
fi
if [[ -n "$PARSER_ENABLED" ]]; then
  case "$PARSER_ENABLED" in
    true|false)
      PARSER_ENABLED_OVERRIDE="$PARSER_ENABLED"
      if [[ "$PARSER_ENABLED" == "false" ]]; then
        PARSER_PROVIDER_OVERRIDE=""
      elif [[ -z "$PARSER_PROVIDER_OVERRIDE" ]]; then
        PARSER_PROVIDER_OVERRIDE="heuristic"
      fi
      ;;
    *)
      echo "ERROR: --parser-enabled must be true or false"
      exit 1
      ;;
  esac
fi
if [[ -n "$PARSER_PROVIDER" ]]; then
  case "$PARSER_PROVIDER" in
    heuristic|ollama|openrouter)
      PARSER_PROVIDER_OVERRIDE="$PARSER_PROVIDER"
      if [[ -z "$PARSER_ENABLED_OVERRIDE" ]]; then
        PARSER_ENABLED_OVERRIDE="true"
      fi
      ;;
    *)
      echo "ERROR: --parser-provider must be one of: heuristic, ollama, openrouter"
      exit 1
      ;;
  esac
fi

resolve_profile_path() {
  if [[ -n "$CONFIG_PROFILE" ]]; then
    printf '%s\n' "$CONFIG_PROFILE"
    return
  fi
  if [[ "$ENTITY_FACT_BACKEND" == "neo4j" && "$BACKEND" == "qdrant" && "$EMBEDDING_PROVIDER" == "lexical" ]]; then
    if [[ "$PARSER_PROVIDER_OVERRIDE" == "openrouter" ]]; then
      printf 'test/config/providers/qdrant-neo4j-lexical-openrouter.yaml\n'
    else
      printf 'test/config/providers/qdrant-neo4j-lexical.yaml\n'
    fi
    return
  fi
  case "${BACKEND}:${EMBEDDING_PROVIDER}" in
    qdrant:ollama)
      printf 'test/config/providers/qdrant-ollama.yaml\n'
      ;;
    *:openrouter)
      printf 'test/config/providers/openrouter.yaml\n'
      ;;
    *:mock)
      printf 'test/config/providers/mock.yaml\n'
      ;;
    *:lexical)
      printf 'test/config/providers/lexical.yaml\n'
      ;;
    *:ollama)
      printf 'test/config/providers/ollama.yaml\n'
      ;;
    *)
      printf 'test/config/providers/ollama.yaml\n'
      ;;
  esac
}

if ! [[ "$TOP_K" =~ ^[0-9]+$ ]] || [[ "$TOP_K" -le 0 ]]; then
  echo "ERROR: --top-k must be a positive integer"
  exit 1
fi

if ! [[ "$MAX_QUERIES" =~ ^-?[0-9]+$ ]]; then
  echo "ERROR: --max-queries must be an integer"
  exit 1
fi

if ! [[ "$QUERY_WORDS" =~ ^[0-9]+$ ]] || [[ "$QUERY_WORDS" -le 0 ]]; then
  echo "ERROR: --query-words must be a positive integer"
  exit 1
fi

if ! [[ "$SAMPLE_SEED" =~ ^-?[0-9]+$ ]]; then
  echo "ERROR: --sample-seed must be an integer"
  exit 1
fi

mkdir -p "$OUT_DIR"

tmp_dir="$(mktemp -d)"
server_pid=""
server_log="$tmp_dir/server.log"
tmp_tenants="$tmp_dir/tenants.txt"
tmp_fixture_entries="$tmp_dir/fixture_entries.jsonl"
tmp_idx_to_id_tsv="$tmp_dir/idx_to_id.tsv"
tmp_idx_to_id_json="$tmp_dir/idx_to_id.json"
tmp_idx_catalog_json="$tmp_dir/idx_catalog.json"
tmp_idx_target_map_json="$tmp_dir/idx_target_map.json"
tmp_auto_eval_jsonl="$tmp_dir/eval_auto.jsonl"
tmp_eval_jsonl="$tmp_dir/eval_cases.jsonl"
tmp_eval_selected_jsonl="$tmp_dir/eval_cases_selected.jsonl"
tmp_metrics_tsv="$tmp_dir/metrics.tsv"
tmp_chunk_entries_json="$tmp_dir/chunk_entries.json"
tmp_chunk_response_json="$tmp_dir/chunk_response.json"
tmp_chunk_payload_json="$tmp_dir/chunk_payload.json"
tmp_search_payload_json="$tmp_dir/search_payload.json"
sqlite_db_file=""
rendered_cfg_path=""
config_profile_path=""

cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" >/dev/null 2>&1 || true
    wait "$server_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

wait_for_health() {
  local url="$1"
  local timeout_s="$2"
  local start now elapsed
  start="$(date +%s)"
  while true; do
    if curl -sS -f "$url/health" >/dev/null 2>&1; then
      return 0
    fi
    now="$(date +%s)"
    elapsed=$((now - start))
    if (( elapsed >= timeout_s )); then
      return 1
    fi
    sleep 0.25
  done
}

check_ollama_ready() {
  local base_url="$1"
  local model="$2"
  if ! curl -sS -f "$base_url/api/version" >/dev/null; then
    echo "ERROR: Ollama is not reachable at $base_url"
    echo "  Start Ollama with: ollama serve"
    echo "  Install guide: https://ollama.com/download"
    exit 1
  fi
  if ! curl -sS -f "$base_url/api/tags" | jq -e --arg model "$model" 'any(.models[]?; .name==$model or (.name|startswith($model + ":")))' >/dev/null; then
    echo "ERROR: Ollama embedding model '$model' is not available"
    echo "  Pull it with: ollama pull $model"
    exit 1
  fi
}

check_qdrant_ready() {
  local base_url="$1"
  if ! curl -sS -f "$base_url/collections" >/dev/null; then
    echo "ERROR: Qdrant is not reachable at $base_url"
    echo "  Start Qdrant before running backend=qdrant retrieval quality eval."
    exit 1
  fi
}

file_sha256() {
  local path="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$path" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$path" | awk '{print $1}'
    return
  fi
  printf 'unavailable\n'
}

resolve_parser_enabled_effective() {
  if [[ -n "$PARSER_ENABLED_OVERRIDE" ]]; then
    local override
    override="$(printf '%s' "$PARSER_ENABLED_OVERRIDE" | tr '[:upper:]' '[:lower:]' | tr -d '\r' | xargs)"
    case "$override" in
      true|false)
        printf '%s\n' "$override"
        return
        ;;
    esac
  fi

  if [[ -n "$rendered_cfg_path" && -f "$rendered_cfg_path" ]]; then
    local from_rendered
    from_rendered="$(
      awk '
        BEGIN { in_parser=0 }
        /^[^[:space:]]/ { in_parser=0 }
        /^parser:[[:space:]]*$/ { in_parser=1; next }
        in_parser && /^[[:space:]]+enabled:[[:space:]]*/ {
          val=$0
          sub(/^[[:space:]]+enabled:[[:space:]]*/, "", val)
          gsub(/[[:space:]]+$/, "", val)
          print tolower(val)
          exit
        }
      ' "$rendered_cfg_path"
    )"
    from_rendered="$(printf '%s' "$from_rendered" | tr -d '\r' | xargs)"
    case "$from_rendered" in
      true|false)
        printf '%s\n' "$from_rendered"
        return
        ;;
    esac
  fi

  printf 'unknown\n'
}

timestamp_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
run_id="$(date -u +%Y%m%dT%H%M%SZ)"
fixture_name="$(basename "$FIXTURE")"
machine="$(uname -m)"
os_name="$(uname -s)"
run_dir="$OUT_DIR/$run_id"
trace_json="$run_dir/trace.json"
mkdir -p "$run_dir"
raw_mode=false
run_profile="standard"
if [[ "$EMBEDDING_PROVIDER" == "lexical" || "$EMBEDDING_PROVIDER" == "mock" ]]; then
  raw_mode=true
  run_profile="raw_no_ollama"
fi
qdrant_collection_base="$QDRANT_COLLECTION"
qdrant_collection_effective="$QDRANT_COLLECTION"
qdrant_namespace_mode="shared"
if [[ "$BACKEND" == "qdrant" ]]; then
  isolate_qdrant=false
  case "$QDRANT_ISOLATE_RUN" in
    true)
      isolate_qdrant=true
      ;;
    false)
      isolate_qdrant=false
      ;;
    auto)
      if [[ "$START_SERVER" -eq 1 ]]; then
        isolate_qdrant=true
      fi
      ;;
  esac
  if [[ "$isolate_qdrant" == "true" ]]; then
    qdrant_namespace_mode="isolated"
    qdrant_collection_effective="${QDRANT_COLLECTION}_${run_id,,}"
  fi
fi

eval_target_mode_resolved="$EVAL_TARGET_MODE"
eval_label_strategy="raw_turn_ids"
eval_search_kinds_json='[]'

if [[ "$START_SERVER" -eq 1 ]]; then
  if [[ "$EMBEDDING_PROVIDER" == "ollama" ]]; then
    check_ollama_ready "$OLLAMA_BASE_URL" "$OLLAMA_MODEL"
  fi
  if [[ "$BACKEND" == "qdrant" ]]; then
    check_qdrant_ready "$QDRANT_BASE_URL"
  fi
  BASE_URL="http://${HOST}:${PORT}"
  if curl -sS -f "$BASE_URL/health" >/dev/null 2>&1; then
    echo "ERROR: refusing to start a new server because ${BASE_URL}/health is already responding."
    echo "  This usually means a stale server is running and would contaminate this run."
    echo "  Stop that server or use --base-url to target it intentionally."
    exit 1
  fi
  db_path="$tmp_dir/retrieval_eval.sqlite"
  sqlite_db_file="$db_path"
  sqlite_db_path="$db_path"
  if command -v cygpath >/dev/null 2>&1; then
    if converted_path="$(cygpath -m "$db_path" 2>/dev/null)"; then
      sqlite_db_path="$converted_path"
    fi
  fi
  cfg_path="$tmp_dir/retrieval_eval.yaml"
  rendered_cfg_path="$cfg_path"
  config_profile_path="$(resolve_profile_path)"
  if [[ ! -f "$config_profile_path" ]]; then
    echo "ERROR: config profile file not found: $config_profile_path"
    exit 1
  fi
  go run ./cmd/configrender \
    -profile "$config_profile_path" \
    -out "$cfg_path" \
    -host "$HOST" \
    -port "$PORT" \
    -vector-backend "$BACKEND" \
    ${ENTITY_FACT_BACKEND:+-entity-fact-backend} \
    ${ENTITY_FACT_BACKEND:+$ENTITY_FACT_BACKEND} \
    -sqlite-dsn "file:${sqlite_db_path}?cache=shared" \
    -qdrant-url "$QDRANT_BASE_URL" \
    -qdrant-api-key "$QDRANT_API_KEY" \
    -qdrant-collection "$qdrant_collection_effective" \
    -qdrant-timeout-ms "$QDRANT_TIMEOUT_MS" \
    -embedding-provider "$EMBEDDING_PROVIDER" \
    ${PARSER_ENABLED_OVERRIDE:+-parser-enabled} \
    ${PARSER_ENABLED_OVERRIDE:+$PARSER_ENABLED_OVERRIDE} \
    ${PARSER_PROVIDER_OVERRIDE:+-parser-provider} \
    ${PARSER_PROVIDER_OVERRIDE:+$PARSER_PROVIDER_OVERRIDE} \
    ${PARSER_OPENROUTER_MODEL:+-parser-openrouter-model} \
    ${PARSER_OPENROUTER_MODEL:+$PARSER_OPENROUTER_MODEL} \
    -embedding-ollama-url "$OLLAMA_BASE_URL" \
    -embedding-ollama-model "$OLLAMA_MODEL" \
    -embedding-ollama-timeout-seconds "$OLLAMA_TIMEOUT_SECONDS" \
    -embedding-model-path "$ONNX_MODEL_PATH" \
    -embedding-tokenizer-path "$ONNX_TOKENIZER_PATH"

  echo "==> Starting retrieval quality server on ${BASE_URL}"
  export GOCACHE="${GOCACHE:-$tmp_dir/gocache}"
  go run ./cmd/pali -config "$cfg_path" >"$server_log" 2>&1 &
  server_pid="$!"

  if ! wait_for_health "$BASE_URL" 30; then
    echo "ERROR: server did not become healthy in time"
    echo "---- server log ----"
    cat "$server_log"
    exit 1
  fi
else
  echo "==> Using existing server at ${BASE_URL}"
  if ! wait_for_health "$BASE_URL" 5; then
    echo "ERROR: health check failed at ${BASE_URL}/health"
    exit 1
  fi
fi

if [[ -n "$config_profile_path" && -f "$config_profile_path" ]]; then
  cp "$config_profile_path" "$run_dir/config.profile.yaml"
fi
if [[ -n "$rendered_cfg_path" && -f "$rendered_cfg_path" ]]; then
  cp "$rendered_cfg_path" "$run_dir/config.rendered.yaml"
fi

if [[ "$eval_target_mode_resolved" == "auto" ]]; then
  parser_enabled_effective="$(resolve_parser_enabled_effective)"
  if [[ "$parser_enabled_effective" == "true" ]]; then
    eval_target_mode_resolved="canonical"
  else
    eval_target_mode_resolved="raw_turn"
    if [[ "$parser_enabled_effective" == "unknown" ]]; then
      echo "WARN: parser enabled state unavailable in auto mode; defaulting eval target to raw_turn"
    fi
  fi
fi
if [[ -z "$EVAL_SET" && "$eval_target_mode_resolved" != "raw_turn" ]]; then
  echo "WARN: eval target '$eval_target_mode_resolved' requires --eval-set; falling back to raw_turn"
  eval_target_mode_resolved="raw_turn"
fi
case "$eval_target_mode_resolved" in
  raw_turn)
    eval_label_strategy="raw_turn_ids"
    eval_search_kinds_json='[]'
    ;;
  source_family)
    eval_label_strategy="source_family_ids"
    eval_search_kinds_json='[]'
    ;;
  canonical)
    eval_label_strategy="canonical_non_raw_ids"
    eval_search_kinds_json='["observation","event","summary"]'
    ;;
esac

fixture_count="$(jq 'length' "$FIXTURE")"
if [[ "$fixture_count" -le 0 ]]; then
  echo "ERROR: fixture is empty: $FIXTURE"
  exit 1
fi
fixture_sha256="$(file_sha256 "$FIXTURE")"
eval_set_sha256=""
if [[ -n "$EVAL_SET" ]]; then
  eval_set_sha256="$(file_sha256 "$EVAL_SET")"
fi
config_profile_sha256=""
if [[ -f "$run_dir/config.profile.yaml" ]]; then
  config_profile_sha256="$(file_sha256 "$run_dir/config.profile.yaml")"
fi
rendered_config_sha256=""
if [[ -f "$run_dir/config.rendered.yaml" ]]; then
  rendered_config_sha256="$(file_sha256 "$run_dir/config.rendered.yaml")"
fi

jq -c 'to_entries[] | {idx:(.key|tonumber), tenant_id:.value.tenant_id, content:(.value.content | gsub("\\s+";" ")), payload:.value}' "$FIXTURE" > "$tmp_fixture_entries"
jq -r '.[].tenant_id' "$FIXTURE" | tr -d '\r' | sort -u > "$tmp_tenants"
tenant_count="$(wc -l < "$tmp_tenants" | tr -d ' ')"

echo "==> Retrieval quality run"
echo "    fixture      : $FIXTURE (${fixture_count} memories, ${tenant_count} tenants)"
echo "    fixture sha  : $fixture_sha256"
echo "    backend      : $BACKEND"
if [[ "$BACKEND" == "qdrant" ]]; then
  echo "    qdrant base  : $QDRANT_BASE_URL"
  echo "    qdrant coll  : $qdrant_collection_effective"
  echo "    qdrant mode  : $qdrant_namespace_mode"
fi
if [[ -n "$ENTITY_FACT_BACKEND" ]]; then
  echo "    fact backend : $ENTITY_FACT_BACKEND"
  if [[ -n "$PARSER_ENABLED_OVERRIDE" ]]; then
    if [[ "$PARSER_ENABLED_OVERRIDE" == "true" ]]; then
      echo "    parser       : enabled"
    else
      echo "    parser       : disabled"
    fi
  fi
fi
echo "    embedder     : $EMBEDDING_PROVIDER"
if [[ "$EMBEDDING_PROVIDER" == "ollama" ]]; then
  echo "    ollama model : $OLLAMA_MODEL"
fi
echo "    top_k        : $TOP_K"
echo "    max_queries  : $MAX_QUERIES"
echo "    eval target  : $eval_target_mode_resolved"
echo "    label strat  : $eval_label_strategy"
if [[ "$eval_search_kinds_json" != "[]" ]]; then
  echo "    search kinds : $(jq -r 'join(",")' <<<"$eval_search_kinds_json")"
fi
echo "    config prof  : ${config_profile_path:-"(external server)"}"
echo "    run profile  : $run_profile"
echo "    run dir      : $run_dir"
if [[ -n "$EVAL_SET" ]]; then
  echo "    eval set     : $EVAL_SET"
  echo "    eval set sha : $eval_set_sha256"
else
  echo "    eval set     : auto-generated (grouped by tenant+query with multi-relevant IDs)"
fi
if [[ "$TOP_K" -ne 5 || -z "$EVAL_SET" ]]; then
  echo "    gate note    : exploratory run (official gates require top_k=5 + labeled eval set)"
fi
echo "    output dir   : $OUT_DIR"
echo ""

echo "==> Creating tenants"
while IFS= read -r tenant_id; do
  tenant_id="${tenant_id%$'\r'}"
  payload="$(jq -n --arg id "$tenant_id" --arg name "$tenant_id" '{id:$id,name:$name}')"
  http_code="$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST "$BASE_URL/v1/tenants" \
    -H 'Content-Type: application/json' \
    --data "$payload")"
  if [[ "$http_code" != "201" && "$http_code" != "409" ]]; then
    echo "ERROR: failed creating tenant '$tenant_id' (HTTP $http_code)"
    exit 1
  fi
done < "$tmp_tenants"

echo "==> Storing fixture memories (${fixture_count} ops, chunk_size=${STORE_CHUNK_SIZE})"
store_ok=0
store_fail=0
i=0
chunk_tmp="$tmp_dir/chunk.jsonl"
> "$chunk_tmp"
chunk_line_count=0

flush_store_chunk() {
  [[ $chunk_line_count -eq 0 ]] && return

  # Build {"items":[...]} from the buffered payloads.
  local items_json response http_code body n_ok payload_path
  items_json="$(jq -rsc '[.[].payload]' < "$chunk_tmp")"
  payload_path="$tmp_chunk_payload_json"
  printf '{"items":%s}\n' "$items_json" > "$payload_path"
  response="$(curl -sS -w '\n%{http_code}' \
    -X POST "$BASE_URL/v1/memory/batch" \
    -H 'Content-Type: application/json' \
    --data-binary "@${payload_path}")"
  http_code="$(printf '%s\n' "$response" | tail -n1 | tr -d '\r')"
  body="$(printf '%s\n' "$response" | sed '$d')"

  if [[ "$http_code" == "201" ]]; then
    # Process each entry matched by position with the response items.
    jq -sc '.' < "$chunk_tmp" > "$tmp_chunk_entries_json"
    printf '%s\n' "$body" > "$tmp_chunk_response_json"

    # Write idxâ†’id TSV (one tab-separated line per successfully stored entry).
    n_ok=0
    while IFS= read -r idx_line; do
      printf '%s\n' "$idx_line" >> "$tmp_idx_to_id_tsv"
      n_ok=$((n_ok + 1))
    done < <(
      jq -rn \
        --slurpfile entries "$tmp_chunk_entries_json" \
        --slurpfile resp "$tmp_chunk_response_json" '
        ($entries[0] // []) | to_entries[] |
        .key as $j | .value as $e |
        (($resp[0].items[$j] // {}) | .id // "") as $mid |
        select($mid != "") |
        [$e.idx, $mid] | @tsv
      '
    )
    store_ok=$((store_ok + n_ok))
    store_fail=$((store_fail + chunk_line_count - n_ok))

    # Write auto-eval JSONL entries.
    while IFS= read -r eval_line; do
      printf '%s\n' "$eval_line" >> "$tmp_auto_eval_jsonl"
    done < <(
      jq -rn \
        --slurpfile entries "$tmp_chunk_entries_json" \
        --slurpfile resp "$tmp_chunk_response_json" \
        --argjson n "$QUERY_WORDS" '
        ($entries[0] // []) | to_entries[] |
        .key as $j | .value as $e |
        (($resp[0].items[$j] // {}) | .id // "") as $mid |
        select($mid != "") |
        ($e.content | split(" ") | .[:$n] | join(" ")) as $raw_q |
        (if ($raw_q | ltrimstr(" ") | length) == 0 then "user preference" else $raw_q end) as $q |
        {tenant_id: $e.tenant_id, query: $q, expected_ids: [$mid]} | tojson
      '
    )
  else
    store_fail=$((store_fail + chunk_line_count))
  fi

  > "$chunk_tmp"
  chunk_line_count=0
}

while IFS= read -r entry_json; do
  i=$((i + 1))
  printf '%s\n' "$entry_json" >> "$chunk_tmp"
  chunk_line_count=$((chunk_line_count + 1))

  if (( chunk_line_count >= STORE_CHUNK_SIZE )); then
    flush_store_chunk
  fi

  if (( i % 50 == 0 || i == fixture_count )); then
    printf "\r  [%d/%d]" "$i" "$fixture_count"
  fi
done < "$tmp_fixture_entries"
flush_store_chunk
printf "\n"

if [[ "$store_ok" -eq 0 ]]; then
  echo "ERROR: no memories were stored successfully; cannot evaluate retrieval quality"
  exit 1
fi

jq -Rn 'reduce inputs as $line ({}; ($line | split("\t")) as $p | if ($p|length) >= 2 then . + {($p[0]): $p[1]} else . end)' \
  < "$tmp_idx_to_id_tsv" > "$tmp_idx_to_id_json"

# Build an index catalog with all/raw/canonical IDs per fixture index.
if [[ -f "$tmp_idx_to_id_json" && -n "$sqlite_db_file" && -f "$sqlite_db_file" ]]; then
  if ! go run ./cmd/evalidmap -db "$sqlite_db_file" -id-map "$tmp_idx_to_id_json" -out "$tmp_idx_catalog_json" >/dev/null; then
    echo "WARN: evalidmap enrichment failed; falling back to raw-only fixture index map"
  fi
fi
if [[ ! -f "$tmp_idx_catalog_json" ]]; then
  jq -n --slurpfile idmap "$tmp_idx_to_id_json" '
    {
      all_by_index: ($idmap[0] // {}),
      raw_by_index: ($idmap[0] // {}),
      canonical_by_index: {}
    }
  ' > "$tmp_idx_catalog_json"
fi

case "$eval_target_mode_resolved" in
  raw_turn)
    jq -n --slurpfile c "$tmp_idx_catalog_json" '
      ($c[0].raw_by_index // {}) as $raw |
      ($c[0].all_by_index // {}) as $all |
      reduce ((($raw | keys_unsorted) + ($all | keys_unsorted)) | unique[]) as $k
        ({}; .[$k] = (($raw[$k] // $all[$k] // []) | map(select(type=="string" and length>0)) | unique))
    ' > "$tmp_idx_target_map_json"
    ;;
  source_family)
    jq -n --slurpfile c "$tmp_idx_catalog_json" '
      ($c[0].all_by_index // {}) |
      with_entries(.value |= ((. // []) | map(select(type=="string" and length>0)) | unique))
    ' > "$tmp_idx_target_map_json"
    ;;
  canonical)
    jq -n --slurpfile c "$tmp_idx_catalog_json" '
      ($c[0].canonical_by_index // {}) |
      with_entries(.value |= ((. // []) | map(select(type=="string" and length>0)) | unique))
    ' > "$tmp_idx_target_map_json"
    ;;
esac

eval_case_input_count=0
if [[ -n "$EVAL_SET" ]]; then
  eval_case_input_count="$(jq '[.[] | select((.tenant_id // "" | length) > 0 and (.query // "" | length) > 0)] | length' "$EVAL_SET")"
  jq -c --slurpfile idmap "$tmp_idx_target_map_json" '
    def arr($x): ($x // []) | if type == "array" then . else [] end;
    .[] |
    {
      tenant_id: (.tenant_id // ""),
      query: (.query // ""),
      expected_ids: (
        if (arr(.expected_memory_ids) | length) > 0 then
          arr(.expected_memory_ids)
        elif (arr(.expected_fixture_indexes) | length) > 0 then
          (
            [arr(.expected_fixture_indexes)[] | tostring | ($idmap[0][.] // empty)]
            | [ .[] | if type == "array" then .[] else . end | select(type == "string" and (length > 0)) ]
            | unique
          )
        else
          []
        end
      )
    } |
    select((.tenant_id|length) > 0 and (.query|length) > 0 and (.expected_ids|length) > 0)
  ' "$EVAL_SET" > "$tmp_eval_jsonl"
else
  # Auto mode can produce repeated tenant/query pairs (e.g., common sentence prefixes).
  # Group those into one eval case with multiple relevant IDs to avoid label collisions.
  jq -cs '
    sort_by(.tenant_id, .query) |
    group_by(.tenant_id + "\u0000" + .query) |
    .[] |
    {
      tenant_id: .[0].tenant_id,
      query: .[0].query,
      expected_ids: ([.[].expected_ids[]] | unique)
    } |
    select((.tenant_id|length) > 0 and (.query|length) > 0 and (.expected_ids|length) > 0)
  ' "$tmp_auto_eval_jsonl" > "$tmp_eval_jsonl"
fi

eval_case_count="$(wc -l < "$tmp_eval_jsonl" | tr -d ' ')"
if [[ "$eval_case_count" -le 0 ]]; then
  echo "ERROR: no valid eval cases found"
  exit 1
fi
eval_cases_dropped_no_target=0
if [[ "$eval_case_input_count" -gt "$eval_case_count" ]]; then
  eval_cases_dropped_no_target=$((eval_case_input_count - eval_case_count))
fi

eval_mode="labeled_${eval_target_mode_resolved}"
auto_ambiguous_cases=0
if [[ -z "$EVAL_SET" ]]; then
  eval_mode="auto_prefix_grouped_${eval_target_mode_resolved}"
  auto_ambiguous_cases="$(jq -cs '[.[] | select((.expected_ids|length) > 1)] | length' "$tmp_eval_jsonl")"
fi

selected_queries="$eval_case_count"
if [[ "$MAX_QUERIES" -gt 0 && "$MAX_QUERIES" -lt "$eval_case_count" ]]; then
  selected_queries="$MAX_QUERIES"
fi
if [[ "$selected_queries" -lt "$eval_case_count" ]]; then
  awk -v seed="$SAMPLE_SEED" 'BEGIN{srand(seed)} {printf "%.17f\t%s\n", rand(), $0}' "$tmp_eval_jsonl" \
    | sort -n \
    | head -n "$selected_queries" \
    | cut -f2- > "$tmp_eval_selected_jsonl"
else
  cp "$tmp_eval_jsonl" "$tmp_eval_selected_jsonl"
fi

echo "==> Evaluating retrieval quality (${selected_queries} queries)"
eval_ok=0
eval_fail=0
q=0
while IFS= read -r eval_case; do
  q=$((q + 1))
  tenant_id="$(jq -r '.tenant_id' <<< "$eval_case")"
  query="$(jq -r '.query' <<< "$eval_case")"
  tenant_id="${tenant_id%$'\r'}"
  query="${query%$'\r'}"
  expected_ids_json="$(jq -c '.expected_ids' <<< "$eval_case")"

  payload="$(jq -n \
    --arg tenant_id "$tenant_id" \
    --arg query "$query" \
    --argjson top_k "$TOP_K" \
    --argjson kinds "$eval_search_kinds_json" '
    {
      tenant_id: $tenant_id,
      query: $query,
      top_k: $top_k,
      disable_touch: true
    } |
    if ($kinds | length) > 0 then . + {kinds: $kinds} else . end
  ')"
  printf '%s\n' "$payload" > "$tmp_search_payload_json"

  response="$(curl -sS -w '\n%{http_code}' \
    -X POST "$BASE_URL/v1/memory/search" \
    -H 'Content-Type: application/json' \
    --data-binary "@${tmp_search_payload_json}")"
  http_code="$(printf '%s\n' "$response" | tail -n1 | tr -d '\r')"
  body="$(printf '%s\n' "$response" | sed '$d')"

  if [[ "$http_code" != "200" ]]; then
    eval_fail=$((eval_fail + 1))
    if (( q % 50 == 0 || q == selected_queries )); then
      printf "\r  [%d/%d]" "$q" "$selected_queries"
    fi
    continue
  fi

  returned_ids_json="$(printf '%s' "$body" | jq -c '.items | map(.id)')"
  metric_json="$(jq -n \
    --argjson returned "$returned_ids_json" \
    --argjson expected "$expected_ids_json" \
    --argjson k "$TOP_K" '
      def at($arr; $i): if $i < ($arr|length) then $arr[$i] else null end;
      [range(0; $k) |
        (at($returned; .) as $id |
          if $id == null then 0
          else (if any($expected[]; . == $id) then 1 else 0 end)
          end)
      ] as $rels
      | ($rels | add) as $hits
      | ($expected | length) as $relevant
      | ($rels
          | to_entries
          | map(if .value == 1 then 1 / (((.key + 2) | log) / (2 | log)) else 0 end)
          | add // 0
        ) as $dcg
      | ([range(0; ([$relevant, $k] | min))
          | 1 / (((. + 2) | log) / (2 | log))
         ] | add // 0) as $idcg
      | {
          top1_hit: (if at($returned; 0) == null then 0 else (if any($expected[]; . == at($returned; 0)) then 1 else 0 end) end),
          recall_at_k: (if $relevant == 0 then 0 else $hits / $relevant end),
          ndcg_at_k: (if $idcg == 0 then 0 else $dcg / $idcg end),
          mrr: (($rels | index(1)) as $first | if $first == null then 0 else 1 / ($first + 1) end),
          hits: $hits,
          relevant: $relevant
        }')"

  top1_hit="$(jq -r '.top1_hit' <<< "$metric_json")"
  recall="$(jq -r '.recall_at_k' <<< "$metric_json")"
  ndcg="$(jq -r '.ndcg_at_k' <<< "$metric_json")"
  mrr="$(jq -r '.mrr' <<< "$metric_json")"
  hits="$(jq -r '.hits' <<< "$metric_json")"
  relevant="$(jq -r '.relevant' <<< "$metric_json")"
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$top1_hit" "$recall" "$ndcg" "$mrr" "$hits" "$relevant" >> "$tmp_metrics_tsv"
  eval_ok=$((eval_ok + 1))

  if (( q % 50 == 0 || q == selected_queries )); then
    printf "\r  [%d/%d]" "$q" "$selected_queries"
  fi
done < "$tmp_eval_selected_jsonl"
printf "\n"

if [[ "$eval_ok" -le 0 ]]; then
  echo "ERROR: no retrieval eval queries completed successfully"
  exit 1
fi

mean_metrics="$(awk -F'\t' '{t+=$1; r+=$2; n+=$3; m+=$4} END{printf "%.6f|%.6f|%.6f|%.6f", t/NR, r/NR, n/NR, m/NR}' "$tmp_metrics_tsv")"
top1_hit_rate="${mean_metrics%%|*}"
rest="${mean_metrics#*|}"
recall_at_k="${rest%%|*}"
rest="${rest#*|}"
ndcg_at_k="${rest%%|*}"
mrr="${rest#*|}"

totals="$(awk -F'\t' '{h+=$5; rel+=$6} END{printf "%.0f|%.0f", h, rel}' "$tmp_metrics_tsv")"
total_hits="${totals%%|*}"
total_relevant="${totals#*|}"

hit_rate_at_k="$(awk -F'\t' '{if($5>0) hit++} END{if(NR==0) print "0.000000"; else printf "%.6f", hit/NR}' "$tmp_metrics_tsv")"
micro_recall_at_k="$(awk -v h="$total_hits" -v rel="$total_relevant" 'BEGIN{if(rel<=0) print "0.000000"; else printf "%.6f", h/rel}')"
average_hits_at_k="$(awk -v h="$total_hits" -v ok="$eval_ok" 'BEGIN{if(ok<=0) print "0.000000"; else printf "%.6f", h/ok}')"
hits_per_relevant="$(awk -v h="$total_hits" -v rel="$total_relevant" 'BEGIN{if(rel<=0) print "0.000000"; else printf "%.6f", h/rel}')"
official_gate_eligible=false
if [[ "$TOP_K" -eq 5 && -n "$EVAL_SET" ]]; then
  official_gate_eligible=true
fi

if [[ -n "$sqlite_db_file" && -f "$sqlite_db_file" ]]; then
  trace_args=(-db "$sqlite_db_file" -out "$trace_json")
  if [[ -f "$tmp_idx_catalog_json" ]]; then
    trace_args+=(-id-catalog "$tmp_idx_catalog_json")
  fi
  if [[ -n "$EVAL_SET" && -f "$EVAL_SET" ]]; then
    trace_args+=(-eval-set "$EVAL_SET")
  fi
  if [[ -n "$rendered_cfg_path" && -f "$rendered_cfg_path" ]]; then
    trace_args+=(-config "$rendered_cfg_path")
  fi
  if ! go run ./cmd/evaltrace "${trace_args[@]}" >/dev/null 2>&1; then
    echo "WARN: failed to generate trace artifact"
    trace_json=""
  fi
else
  trace_json=""
fi

result_json="$run_dir/retrieval_quality.json"
summary_txt="$run_dir/retrieval_quality.summary.txt"

cat > "$result_json" <<EOF
{
  "run_id": "$run_id",
  "run_dir": "$run_dir",
  "run_profile": "$run_profile",
  "raw_mode": $raw_mode,
  "timestamp_utc": "$timestamp_utc",
  "backend": "$BACKEND",
  "qdrant_namespace_mode": "$qdrant_namespace_mode",
  "qdrant_collection_base": "$qdrant_collection_base",
  "qdrant_collection_effective": "$qdrant_collection_effective",
  "fixture": "$FIXTURE",
  "fixture_sha256": "$fixture_sha256",
  "fixture_count": $fixture_count,
  "tenant_count": $tenant_count,
  "base_url": "$BASE_URL",
  "embedding_provider": "$EMBEDDING_PROVIDER",
  "embedding_model": "$OLLAMA_MODEL",
  "machine": "$machine",
  "os": "$os_name",
  "top_k": $TOP_K,
  "query_words": $QUERY_WORDS,
  "sample_seed": $SAMPLE_SEED,
  "eval_target_mode": "$eval_target_mode_resolved",
  "eval_label_strategy": "$eval_label_strategy",
  "eval_search_kinds": $eval_search_kinds_json,
  "eval_mode": "$eval_mode",
  "auto_ambiguous_cases": $auto_ambiguous_cases,
  "eval_cases_dropped_no_target": $eval_cases_dropped_no_target,
  "config_profile": "${config_profile_path}",
  "config_profile_sha256": "${config_profile_sha256}",
  "rendered_config": "${rendered_cfg_path}",
  "rendered_config_sha256": "${rendered_config_sha256}",
  "eval_set": "${EVAL_SET}",
  "eval_set_sha256": "${eval_set_sha256}",
  "eval_cases_total": $eval_case_count,
  "eval_cases_selected": $selected_queries,
  "eval_success": $eval_ok,
  "eval_failures": $eval_fail,
  "official_gate_eligible": $official_gate_eligible,
  "trace_json": "${trace_json}",
  "metrics": {
    "top1_hit_rate": $top1_hit_rate,
    "topk_accuracy": $hit_rate_at_k,
    "recall_at_k": $recall_at_k,
    "ndcg_at_k": $ndcg_at_k,
    "mrr": $mrr,
    "hit_rate_at_k": $hit_rate_at_k,
    "micro_recall_at_k": $micro_recall_at_k,
    "average_hits_at_k": $average_hits_at_k,
    "hits_per_relevant": $hits_per_relevant,
    "total_hits_at_k": $total_hits,
    "total_relevant": $total_relevant
  }
}
EOF

cat > "$summary_txt" <<EOF
Pali Retrieval Quality Summary
==============================
Run ID         : $run_id
Run dir        : $run_dir
Run profile    : $run_profile
Timestamp (UTC): $timestamp_utc
Backend        : $BACKEND
Qdrant mode    : $qdrant_namespace_mode
Qdrant coll    : $qdrant_collection_effective
Embedder       : $EMBEDDING_PROVIDER
Embed model    : $OLLAMA_MODEL
Fixture        : $FIXTURE
Fixture SHA256 : $fixture_sha256
Fixture count  : $fixture_count
Tenant count   : $tenant_count
Base URL       : $BASE_URL
Machine        : $machine
OS             : $os_name

Evaluation
----------
top_k          : $TOP_K
query_words    : $QUERY_WORDS
sample_seed    : $SAMPLE_SEED
eval_target    : $eval_target_mode_resolved
label_strategy : $eval_label_strategy
eval_mode      : $eval_mode
eval_set       : ${EVAL_SET:-"(auto-generated from fixture)"}
eval_set_sha256: ${eval_set_sha256:-"(n/a)"}
Config profile : ${config_profile_path:-"(external server)"}
Config SHA256  : ${config_profile_sha256:-"(n/a)"}
Rendered config: ${rendered_cfg_path:-"(external server)"}
Rendered SHA256: ${rendered_config_sha256:-"(n/a)"}
Cases (total)  : $eval_case_count
Cases (run)    : $selected_queries
Success/Fail   : $eval_ok / $eval_fail
Dropped(no ids): $eval_cases_dropped_no_target
Auto ambiguous : $auto_ambiguous_cases
Gate eligible  : $official_gate_eligible (requires top_k=5 and labeled eval-set)

Retrieval Metrics
-----------------
Top1HitRate       : $top1_hit_rate
Top${TOP_K}Accuracy   : $hit_rate_at_k
Recall@$TOP_K     : $recall_at_k
nDCG@$TOP_K       : $ndcg_at_k
MRR               : $mrr
HitRate@$TOP_K    : $hit_rate_at_k
MicroRecall@$TOP_K: $micro_recall_at_k
AvgHits@$TOP_K    : $average_hits_at_k
Hits/Relevant     : $hits_per_relevant
Hits / Relevant   : $total_hits / $total_relevant

Artifacts
---------
JSON result    : $result_json
Text summary   : $summary_txt
Trace JSON     : ${trace_json:-"(not generated)"}
EOF

echo ""
echo "==> Retrieval quality evaluation complete"
echo "    JSON    : $result_json"
echo "    Summary : $summary_txt"
echo ""
cat "$summary_txt"
