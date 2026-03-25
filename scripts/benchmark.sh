#!/usr/bin/env bash
# benchmark.sh â€” run reproducible API benchmarks and write results to disk.
set -euo pipefail

cd "$(dirname "$0")/.."

FIXTURE="testdata/benchmarks/fixtures/release_memories.json"
EVAL_SET="testdata/benchmarks/evals/release_curated.json"
BACKEND="sqlite"
OUT_DIR="test/benchmarks/results"
SEARCH_OPS=200
TOP_K=5
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
PGVECTOR_DSN=""
PGVECTOR_TABLE="pali_memories"
PGVECTOR_AUTO_MIGRATE="true"
PGVECTOR_MAX_OPEN_CONNS=10
PGVECTOR_MAX_IDLE_CONNS=5
PARSER_ENABLED=""
PARSER_PROVIDER=""
PARSER_OPENROUTER_MODEL=""

usage() {
  cat <<'EOF'
Usage:
  scripts/benchmark.sh [flags]

Flags:
  --fixture <path>         Fixture JSON file (default: testdata/benchmarks/fixtures/release_memories.json)
  --eval-set <path>        Optional labeled eval set used for realistic search queries (default: testdata/benchmarks/evals/release_curated.json)
  --backend <name>         sqlite | qdrant | pgvector (default: sqlite)
  --out-dir <path>         Output directory for JSON + summary results
  --search-ops <n>         Number of search operations (default: 200)
  --top-k <n>              top_k used in search requests (default: 5)
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
  --pgvector-dsn <dsn>     Postgres DSN for pgvector backend
  --pgvector-table <name>  pgvector table name (default: pali_memories)
  --pgvector-auto-migrate <bool>  pgvector auto_migrate (default: true)
  --pgvector-max-open-conns <n>   pgvector max_open_conns (default: 10)
  --pgvector-max-idle-conns <n>   pgvector max_idle_conns (default: 5)
  --parser-enabled <bool>  Force parser on/off (true|false). Overrides neo4j auto-mode.
  --parser-provider <name> heuristic | ollama | openrouter (default: auto/heuristic for neo4j)
  --parser-openrouter-model <name> Override parser.openrouter_model for parser provider openrouter
  --help                   Show this help

Examples:
  scripts/benchmark.sh --fixture testdata/benchmarks/fixtures/release_memories.json --backend sqlite
  scripts/benchmark.sh --fixture testdata/benchmarks/fixtures/release_memories.json --search-ops 500
  scripts/benchmark.sh --base-url http://127.0.0.1:8080 --fixture testdata/benchmarks/fixtures/release_memories.json
  scripts/benchmark.sh --fixture testdata/benchmarks/fixtures/release_memories.json --embedding-provider lexical  # raw mode (no Ollama)
  scripts/benchmark.sh --fixture testdata/benchmarks/fixtures/release_memories.json --embedding-provider openrouter  # requires OPENROUTER_API_KEY
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
    --search-ops)
      SEARCH_OPS="$2"
      shift 2
      ;;
    --top-k)
      TOP_K="$2"
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
    --pgvector-dsn)
      PGVECTOR_DSN="$2"
      shift 2
      ;;
    --pgvector-table)
      PGVECTOR_TABLE="$2"
      shift 2
      ;;
    --pgvector-auto-migrate)
      PGVECTOR_AUTO_MIGRATE="$2"
      shift 2
      ;;
    --pgvector-max-open-conns)
      PGVECTOR_MAX_OPEN_CONNS="$2"
      shift 2
      ;;
    --pgvector-max-idle-conns)
      PGVECTOR_MAX_IDLE_CONNS="$2"
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

if [[ ! -f "$FIXTURE" ]]; then
  echo "ERROR: fixture file not found: $FIXTURE"
  exit 1
fi

if [[ -n "$EVAL_SET" && ! -f "$EVAL_SET" ]]; then
  echo "ERROR: eval set file not found: $EVAL_SET"
  exit 1
fi

if ! [[ "$SEARCH_OPS" =~ ^[0-9]+$ ]] || [[ "$SEARCH_OPS" -le 0 ]]; then
  echo "ERROR: --search-ops must be a positive integer"
  exit 1
fi

if ! [[ "$TOP_K" =~ ^[0-9]+$ ]] || [[ "$TOP_K" -le 0 ]]; then
  echo "ERROR: --top-k must be a positive integer"
  exit 1
fi

case "$BACKEND" in
  sqlite|qdrant|pgvector)
    ;;
  *)
    echo "ERROR: --backend must be one of: sqlite, qdrant, pgvector"
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

# Avoid cross-run vector-size collisions in shared Qdrant collections.
if [[ "$EMBEDDING_PROVIDER" == "lexical" && "$QDRANT_COLLECTION" == "pali_memories" ]]; then
  QDRANT_COLLECTION="pali_memories_lexical"
fi

if [[ "$BACKEND" == "pgvector" && -z "${PGVECTOR_DSN// }" ]]; then
  echo "ERROR: --pgvector-dsn is required when --backend=pgvector"
  exit 1
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

mkdir -p "$OUT_DIR"

tmp_dir="$(mktemp -d)"
server_pid=""
server_log="$tmp_dir/server.log"
config_profile_path=""
rendered_cfg_path=""
tmp_store_lat="$tmp_dir/store_lat_ms.txt"
tmp_search_lat="$tmp_dir/search_lat_ms.txt"
tmp_tenants="$tmp_dir/tenants.txt"
tmp_fixture_lines="$tmp_dir/fixture_lines.jsonl"
tmp_search_pairs="$tmp_dir/search_pairs.tsv"
tmp_batch_payload_json="$tmp_dir/batch_payload.json"

cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" >/dev/null 2>&1 || true
    wait "$server_pid" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

now_ns() {
  local ts
  ts="$(date +%s%N 2>/dev/null || true)"
  if [[ "$ts" =~ ^[0-9]+$ ]]; then
    printf '%s\n' "$ts"
    return
  fi
  if command -v perl >/dev/null 2>&1; then
    perl -MTime::HiRes=time -e 'printf "%.0f\n", time()*1000000000'
    return
  fi
  # Fallback to second precision if neither nanosecond source is available.
  printf '%s000000000\n' "$(date +%s)"
}

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
    echo "  Start Qdrant before running backend=qdrant benchmarks."
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

calc_stats() {
  local file="$1"
  local count
  count="$(wc -l < "$file" | tr -d ' ')"
  if [[ "$count" -eq 0 ]]; then
    echo "0|0|0|0|0|0"
    return
  fi

  local mean min max p50 p95 p99
  mean="$(awk '{s+=$1} END {if (NR==0) print "0"; else printf "%.3f", s/NR}' "$file")"
  min="$(sort -n "$file" | awk 'NR==1{printf "%.3f",$1; exit}')"
  max="$(sort -n "$file" | awk 'END{printf "%.3f",$1}')"

  percentile() {
    local p="$1"
    local n rank
    n="$count"
    rank="$(awk -v n="$n" -v p="$p" 'BEGIN{r=int((p*n)+0.999999); if(r<1) r=1; if(r>n) r=n; print r}')"
    sort -n "$file" | awk -v r="$rank" 'NR==r{printf "%.3f",$1; exit}'
  }

  p50="$(percentile 0.50)"
  p95="$(percentile 0.95)"
  p99="$(percentile 0.99)"
  echo "${mean}|${min}|${max}|${p50}|${p95}|${p99}"
}

timestamp_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
run_id="$(date -u +%Y%m%dT%H%M%SZ)"
fixture_name="$(basename "$FIXTURE")"
machine="$(uname -m)"
os_name="$(uname -s)"
run_dir="$OUT_DIR/$run_id"
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
  db_path="$tmp_dir/bench.sqlite"
  sqlite_db_path="$db_path"
  if command -v cygpath >/dev/null 2>&1; then
    if converted_path="$(cygpath -m "$db_path" 2>/dev/null)"; then
      sqlite_db_path="$converted_path"
    fi
  fi
  cfg_path="$tmp_dir/bench.yaml"
  config_profile_path="$(resolve_profile_path)"
  if [[ ! -f "$config_profile_path" ]]; then
    echo "ERROR: config profile file not found: $config_profile_path"
    exit 1
  fi
  rendered_cfg_path="$cfg_path"
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
    -pgvector-dsn "$PGVECTOR_DSN" \
    -pgvector-table "$PGVECTOR_TABLE" \
    -pgvector-auto-migrate "$PGVECTOR_AUTO_MIGRATE" \
    -pgvector-max-open-conns "$PGVECTOR_MAX_OPEN_CONNS" \
    -pgvector-max-idle-conns "$PGVECTOR_MAX_IDLE_CONNS" \
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

  echo "==> Starting benchmark server on ${BASE_URL}"
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

fixture_count="$(jq 'length' "$FIXTURE")"
if [[ "$fixture_count" -le 0 ]]; then
  echo "ERROR: fixture is empty: $FIXTURE"
  exit 1
fi
fixture_sha256="$(file_sha256 "$FIXTURE")"
eval_set_sha256=""
if [[ -n "$EVAL_SET" && -f "$EVAL_SET" ]]; then
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

jq -r '.[].tenant_id' "$FIXTURE" | tr -d '\r' | sort -u > "$tmp_tenants"
jq -c '.[]' "$FIXTURE" > "$tmp_fixture_lines"
query_source="fixture_mid_window"
if [[ -n "$EVAL_SET" ]]; then
  jq -r '.[] | [.tenant_id, (.query | gsub("\\s+";" "))] | @tsv' "$EVAL_SET" > "$tmp_search_pairs"
  query_source="eval_set"
else
  jq -r '
    .[] |
    (.content | gsub("\\s+";" ") | split(" ")) as $words |
    ($words | length) as $n |
    (if $n <= 12 then
       ($words | join(" "))
     else
       ($words[($n/2|floor)-6:($n/2|floor)+6] | join(" "))
     end) as $q |
    [.tenant_id, $q] | @tsv
  ' "$FIXTURE" > "$tmp_search_pairs"
fi
tenant_count="$(wc -l < "$tmp_tenants" | tr -d ' ')"

echo "==> Benchmark run"
echo "    fixture      : $FIXTURE (${fixture_count} memories, ${tenant_count} tenants)"
echo "    fixture sha  : $fixture_sha256"
echo "    backend      : $BACKEND"
if [[ "$BACKEND" == "qdrant" ]]; then
  echo "    qdrant base  : $QDRANT_BASE_URL"
  echo "    qdrant coll  : $qdrant_collection_effective"
  echo "    qdrant mode  : $qdrant_namespace_mode"
fi
if [[ "$BACKEND" == "pgvector" ]]; then
  pgvector_dsn_safe="$PGVECTOR_DSN"
  if [[ -n "$pgvector_dsn_safe" ]]; then
    pgvector_dsn_safe="$(printf '%s' "$pgvector_dsn_safe" | sed -E 's#(://[^:/]+:)[^@]+@#\\1****@#')"
  fi
  echo "    pgvector dsn : $pgvector_dsn_safe"
  echo "    pgvector tbl : $PGVECTOR_TABLE"
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
echo "    search ops   : $SEARCH_OPS"
echo "    top_k        : $TOP_K"
echo "    query source : $query_source"
echo "    config prof  : ${config_profile_path:-"(external server)"}"
echo "    run profile  : $run_profile"
echo "    run dir      : $run_dir"
if [[ -n "$EVAL_SET" ]]; then
  echo "    eval set     : $EVAL_SET"
  echo "    eval set sha : $eval_set_sha256"
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

echo "==> Store benchmark (${fixture_count} ops)"
store_ok=0
store_fail=0
store_start_ns="$(now_ns)"
batch_probe_code="$(curl -sS -o /dev/null -w '%{http_code}' \
  -X POST "$BASE_URL/v1/memory/batch" \
  -H 'Content-Type: application/json' \
  --data '{"items":[]}' | tr -d '\r')"
store_batch_supported=0
store_batch_size=64
store_batch_fallbacks=0
if [[ "$batch_probe_code" != "404" && "$batch_probe_code" != "405" ]]; then
  store_batch_supported=1
fi

if [[ "$store_batch_supported" -eq 1 ]]; then
  store_mode="batch"
  echo "    mode         : batch endpoint (/v1/memory/batch)"
  memory_rows=()
  while IFS= read -r line; do
    memory_rows+=("$line")
  done < "$tmp_fixture_lines"
  i=0
  total="${#memory_rows[@]}"
  while (( i < total )); do
    batch_count=$store_batch_size
    if (( i + batch_count > total )); then
      batch_count=$((total - i))
    fi
    payload="$(printf '%s\n' "${memory_rows[@]:i:batch_count}" | jq -s '{items:.}')"
    printf '%s\n' "$payload" > "$tmp_batch_payload_json"
    req_start_ns="$(now_ns)"
    response="$(curl -sS -X POST "$BASE_URL/v1/memory/batch" \
      -H 'Content-Type: application/json' \
      --data-binary "@${tmp_batch_payload_json}" \
      -w '\n%{http_code}')"
    req_end_ns="$(now_ns)"
    latency_ms="$(awk -v s="$req_start_ns" -v e="$req_end_ns" 'BEGIN{printf "%.3f",(e-s)/1000000}')"
    response_body="$(printf '%s\n' "$response" | sed '$d')"
    http_code="$(printf '%s\n' "$response" | tail -n1 | tr -d '\r')"
    if [[ "$http_code" == "201" ]]; then
      stored_count="$(printf '%s' "$response_body" | jq -r '.items | length' 2>/dev/null || echo "0")"
      if [[ "$stored_count" =~ ^[0-9]+$ ]] && (( stored_count == batch_count )); then
        store_ok=$((store_ok + stored_count))
      else
        store_batch_fallbacks=$((store_batch_fallbacks + 1))
        store_fail=$((store_fail + batch_count))
      fi
    else
      store_batch_fallbacks=$((store_batch_fallbacks + 1))
      store_fail=$((store_fail + batch_count))
    fi
    per_item_latency="$(awk -v l="$latency_ms" -v n="$batch_count" 'BEGIN{if(n<=0) printf "%.3f", l; else printf "%.3f", l/n}')"
    for ((j=0; j<batch_count; j++)); do
      echo "$per_item_latency" >> "$tmp_store_lat"
    done
    i=$((i + batch_count))
    if (( i % 50 == 0 || i == fixture_count )); then
      printf "\r  [%d/%d]" "$i" "$fixture_count"
    fi
  done
else
  store_mode="single"
  store_batch_size=0
  i=0
  while IFS= read -r memory_json; do
    i=$((i + 1))
    req_start_ns="$(now_ns)"
    http_code="$(curl -sS -o /dev/null -w '%{http_code}' \
      -X POST "$BASE_URL/v1/memory" \
      -H 'Content-Type: application/json' \
      --data "$memory_json" | tr -d '\r')"
    req_end_ns="$(now_ns)"
    latency_ms="$(awk -v s="$req_start_ns" -v e="$req_end_ns" 'BEGIN{printf "%.3f",(e-s)/1000000}')"
    echo "$latency_ms" >> "$tmp_store_lat"
    if [[ "$http_code" == "201" ]]; then
      store_ok=$((store_ok + 1))
    else
      store_fail=$((store_fail + 1))
    fi
    if (( i % 50 == 0 || i == fixture_count )); then
      printf "\r  [%d/%d]" "$i" "$fixture_count"
    fi
  done < "$tmp_fixture_lines"
fi
store_end_ns="$(now_ns)"
printf "\n"

echo "==> Search benchmark (${SEARCH_OPS} ops)"
search_pairs=()
while IFS= read -r line; do
  line="${line%$'\r'}"
  search_pairs+=("$line")
done < "$tmp_search_pairs"
pair_count="${#search_pairs[@]}"
if [[ "$pair_count" -le 0 ]]; then
  echo "ERROR: no searchable entries found in fixture"
  exit 1
fi

search_ok=0
search_fail=0
search_start_ns="$(now_ns)"
for ((op=0; op<SEARCH_OPS; op++)); do
  idx=$((op % pair_count))
  line="${search_pairs[$idx]}"
  tenant_id="${line%%$'\t'*}"
  query="${line#*$'\t'}"
  tenant_id="${tenant_id%$'\r'}"
  query="${query%$'\r'}"
  if [[ -z "${query// }" ]]; then
    query="memory recall details"
  fi
  payload="$(jq -n --arg tenant_id "$tenant_id" --arg query "$query" --argjson top_k "$TOP_K" \
    '{tenant_id:$tenant_id,query:$query,top_k:$top_k}')"

  req_start_ns="$(now_ns)"
  http_code="$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST "$BASE_URL/v1/memory/search" \
    -H 'Content-Type: application/json' \
    --data "$payload" | tr -d '\r')"
  req_end_ns="$(now_ns)"
  latency_ms="$(awk -v s="$req_start_ns" -v e="$req_end_ns" 'BEGIN{printf "%.3f",(e-s)/1000000}')"
  echo "$latency_ms" >> "$tmp_search_lat"

  if [[ "$http_code" == "200" ]]; then
    search_ok=$((search_ok + 1))
  else
    search_fail=$((search_fail + 1))
  fi

  if (( (op + 1) % 50 == 0 || op + 1 == SEARCH_OPS )); then
    printf "\r  [%d/%d]" "$((op + 1))" "$SEARCH_OPS"
  fi
done
search_end_ns="$(now_ns)"
printf "\n"

store_total_ms="$(awk -v s="$store_start_ns" -v e="$store_end_ns" 'BEGIN{printf "%.3f",(e-s)/1000000}')"
search_total_ms="$(awk -v s="$search_start_ns" -v e="$search_end_ns" 'BEGIN{printf "%.3f",(e-s)/1000000}')"

store_stats="$(calc_stats "$tmp_store_lat")"
search_stats="$(calc_stats "$tmp_search_lat")"

store_mean="${store_stats%%|*}"
store_rest="${store_stats#*|}"
store_min="${store_rest%%|*}"
store_rest="${store_rest#*|}"
store_max="${store_rest%%|*}"
store_rest="${store_rest#*|}"
store_p50="${store_rest%%|*}"
store_rest="${store_rest#*|}"
store_p95="${store_rest%%|*}"
store_p99="${store_rest#*|}"

search_mean="${search_stats%%|*}"
search_rest="${search_stats#*|}"
search_min="${search_rest%%|*}"
search_rest="${search_rest#*|}"
search_max="${search_rest%%|*}"
search_rest="${search_rest#*|}"
search_p50="${search_rest%%|*}"
search_rest="${search_rest#*|}"
search_p95="${search_rest%%|*}"
search_p99="${search_rest#*|}"

store_throughput="$(awk -v ok="$store_ok" -v ms="$store_total_ms" 'BEGIN{if(ms<=0) print "0"; else printf "%.3f", ok/(ms/1000)}')"
search_throughput="$(awk -v ok="$search_ok" -v ms="$search_total_ms" 'BEGIN{if(ms<=0) print "0"; else printf "%.3f", ok/(ms/1000)}')"

result_json="$run_dir/benchmark.json"
summary_txt="$run_dir/benchmark.summary.txt"

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
  "eval_set": "$EVAL_SET",
  "eval_set_sha256": "$eval_set_sha256",
  "query_source": "$query_source",
  "config_profile": "${config_profile_path}",
  "config_profile_sha256": "${config_profile_sha256}",
  "rendered_config": "${rendered_cfg_path}",
  "rendered_config_sha256": "${rendered_config_sha256}",
  "fixture_count": $fixture_count,
  "tenant_count": $tenant_count,
  "base_url": "$BASE_URL",
  "embedding_provider": "$EMBEDDING_PROVIDER",
  "embedding_model": "$OLLAMA_MODEL",
  "machine": "$machine",
  "os": "$os_name",
  "store_diagnostics": {
    "mode": "$store_mode",
    "batch_size": $store_batch_size,
    "batch_fallbacks": $store_batch_fallbacks
  },
  "store": {
    "mode": "$store_mode",
    "operations": $fixture_count,
    "success": $store_ok,
    "failures": $store_fail,
    "duration_ms": $store_total_ms,
    "throughput_ops_sec": $store_throughput,
    "latency_ms": {
      "mean": $store_mean,
      "min": $store_min,
      "max": $store_max,
      "p50": $store_p50,
      "p95": $store_p95,
      "p99": $store_p99
    }
  },
  "search": {
    "operations": $SEARCH_OPS,
    "success": $search_ok,
    "failures": $search_fail,
    "duration_ms": $search_total_ms,
    "throughput_ops_sec": $search_throughput,
    "top_k": $TOP_K,
    "latency_ms": {
      "mean": $search_mean,
      "min": $search_min,
      "max": $search_max,
      "p50": $search_p50,
      "p95": $search_p95,
      "p99": $search_p99
    }
  }
}
EOF

cat > "$summary_txt" <<EOF
Pali Benchmark Summary
======================
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
Eval set       : ${EVAL_SET:-"(none)"}
Eval set SHA256: ${eval_set_sha256:-"(n/a)"}
Query source   : $query_source
Config profile : ${config_profile_path:-"(external server)"}
Config SHA256  : ${config_profile_sha256:-"(n/a)"}
Rendered config: ${rendered_cfg_path:-"(external server)"}
Rendered SHA256: ${rendered_config_sha256:-"(n/a)"}
Fixture count  : $fixture_count
Tenant count   : $tenant_count
Base URL       : $BASE_URL
Machine        : $machine
OS             : $os_name

Store
-----
Mode           : $store_mode
Batch size     : $store_batch_size
Batch fallbacks: $store_batch_fallbacks
Operations     : $fixture_count
Success/Fail   : $store_ok / $store_fail
Duration (ms)  : $store_total_ms
Throughput     : $store_throughput ops/sec
Latency (ms)   : mean=$store_mean min=$store_min max=$store_max p50=$store_p50 p95=$store_p95 p99=$store_p99

Search
------
Operations     : $SEARCH_OPS
Success/Fail   : $search_ok / $search_fail
Duration (ms)  : $search_total_ms
Throughput     : $search_throughput ops/sec
top_k          : $TOP_K
Latency (ms)   : mean=$search_mean min=$search_min max=$search_max p50=$search_p50 p95=$search_p95 p99=$search_p99

Artifacts
---------
JSON result    : $result_json
Text summary   : $summary_txt
EOF

echo ""
echo "==> Benchmark complete"
echo "    JSON    : $result_json"
echo "    Summary : $summary_txt"
echo ""
cat "$summary_txt"
