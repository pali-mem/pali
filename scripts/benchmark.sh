#!/usr/bin/env bash
# benchmark.sh — run reproducible API benchmarks and write results to disk.
set -euo pipefail

cd "$(dirname "$0")/.."

FIXTURE="test/fixtures/memories.json"
BACKEND="sqlite"
OUT_DIR="test/benchmarks/results"
SEARCH_OPS=200
TOP_K=10
HOST="127.0.0.1"
PORT="18080"
BASE_URL=""
START_SERVER=1
EMBEDDING_PROVIDER="ollama"
OLLAMA_BASE_URL="http://127.0.0.1:11434"
OLLAMA_MODEL="all-minilm"
OLLAMA_TIMEOUT_SECONDS=10
ONNX_MODEL_PATH="./models/all-MiniLM-L6-v2/model.onnx"
ONNX_TOKENIZER_PATH="./models/all-MiniLM-L6-v2/tokenizer.json"

usage() {
  cat <<'EOF'
Usage:
  scripts/benchmark.sh [flags]

Flags:
  --fixture <path>         Fixture JSON file (default: test/fixtures/memories.json)
  --backend <name>         sqlite | qdrant | pgvector (default: sqlite)
  --out-dir <path>         Output directory for JSON + summary results
  --search-ops <n>         Number of search operations (default: 200)
  --top-k <n>              top_k used in search requests (default: 10)
  --host <ip>              Server host for auto-start mode (default: 127.0.0.1)
  --port <port>            Server port for auto-start mode (default: 18080)
  --base-url <url>         Use an already-running server, disables auto-start
  --embedding-provider <p> ollama | onnx | mock (default: ollama)
  --embedding-model <name> Ollama model name (default: all-minilm)
  --ollama-url <url>       Ollama base URL (default: http://127.0.0.1:11434)
  --onnx-model <path>      ONNX model path (default: ./models/all-MiniLM-L6-v2/model.onnx)
  --onnx-tokenizer <path>  ONNX tokenizer path (default: ./models/all-MiniLM-L6-v2/tokenizer.json)
  --help                   Show this help

Examples:
  scripts/benchmark.sh --fixture test/fixtures/memories.json --backend sqlite
  scripts/benchmark.sh --fixture test/fixtures/memories.json --search-ops 500
  scripts/benchmark.sh --base-url http://127.0.0.1:8080 --fixture test/fixtures/memories.json
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --fixture)
      FIXTURE="$2"
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

if ! [[ "$SEARCH_OPS" =~ ^[0-9]+$ ]] || [[ "$SEARCH_OPS" -le 0 ]]; then
  echo "ERROR: --search-ops must be a positive integer"
  exit 1
fi

if ! [[ "$TOP_K" =~ ^[0-9]+$ ]] || [[ "$TOP_K" -le 0 ]]; then
  echo "ERROR: --top-k must be a positive integer"
  exit 1
fi

if [[ "$BACKEND" != "sqlite" ]]; then
  echo "ERROR: backend '$BACKEND' is not benchmarkable yet from scripts/benchmark.sh."
  echo "       Current router wiring uses sqlite store only."
  exit 1
fi

case "$EMBEDDING_PROVIDER" in
  ollama|onnx|mock|lexical)
    ;;
  *)
    echo "ERROR: --embedding-provider must be one of: ollama, onnx, mock"
    exit 1
    ;;
esac

mkdir -p "$OUT_DIR"

tmp_dir="$(mktemp -d)"
server_pid=""
server_log="$tmp_dir/server.log"
tmp_store_lat="$tmp_dir/store_lat_ms.txt"
tmp_search_lat="$tmp_dir/search_lat_ms.txt"
tmp_tenants="$tmp_dir/tenants.txt"
tmp_fixture_lines="$tmp_dir/fixture_lines.jsonl"
tmp_search_pairs="$tmp_dir/search_pairs.tsv"

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

if [[ "$START_SERVER" -eq 1 ]]; then
  if [[ "$EMBEDDING_PROVIDER" == "ollama" ]]; then
    check_ollama_ready "$OLLAMA_BASE_URL" "$OLLAMA_MODEL"
  fi
  BASE_URL="http://${HOST}:${PORT}"
  db_path="$tmp_dir/bench.sqlite"
  cfg_path="$tmp_dir/bench.yaml"
  cat > "$cfg_path" <<EOF
server:
  host: "${HOST}"
  port: ${PORT}
vector_backend: "${BACKEND}"
database:
  sqlite_dsn: "file:${db_path}?cache=shared"
embedding:
  provider: "${EMBEDDING_PROVIDER}"
  ollama_base_url: "${OLLAMA_BASE_URL}"
  ollama_model: "${OLLAMA_MODEL}"
  ollama_timeout_seconds: ${OLLAMA_TIMEOUT_SECONDS}
  model_path: "${ONNX_MODEL_PATH}"
  tokenizer_path: "${ONNX_TOKENIZER_PATH}"
auth:
  enabled: false
  jwt_secret: ""
  issuer: "pali"
EOF

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

fixture_count="$(jq 'length' "$FIXTURE")"
if [[ "$fixture_count" -le 0 ]]; then
  echo "ERROR: fixture is empty: $FIXTURE"
  exit 1
fi

jq -r '.[].tenant_id' "$FIXTURE" | sort -u > "$tmp_tenants"
jq -c '.[]' "$FIXTURE" > "$tmp_fixture_lines"
jq -r '.[] | [.tenant_id, (.content | gsub("\\s+";" "))] | @tsv' "$FIXTURE" > "$tmp_search_pairs"
tenant_count="$(wc -l < "$tmp_tenants" | tr -d ' ')"

echo "==> Benchmark run"
echo "    fixture      : $FIXTURE (${fixture_count} memories, ${tenant_count} tenants)"
echo "    backend      : $BACKEND"
echo "    embedder     : $EMBEDDING_PROVIDER"
if [[ "$EMBEDDING_PROVIDER" == "ollama" ]]; then
  echo "    ollama model : $OLLAMA_MODEL"
fi
echo "    search ops   : $SEARCH_OPS"
echo "    top_k        : $TOP_K"
echo "    output dir   : $OUT_DIR"
echo ""

echo "==> Creating tenants"
while IFS= read -r tenant_id; do
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
  --data '{"items":[]}')"
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
    req_start_ns="$(now_ns)"
    response="$(curl -sS -X POST "$BASE_URL/v1/memory/batch" \
      -H 'Content-Type: application/json' \
      --data "$payload" \
      -w '\n%{http_code}')"
    req_end_ns="$(now_ns)"
    latency_ms="$(awk -v s="$req_start_ns" -v e="$req_end_ns" 'BEGIN{printf "%.3f",(e-s)/1000000}')"
    response_body="${response%$'\n'*}"
    http_code="${response##*$'\n'}"
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
      --data "$memory_json")"
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
  content="${line#*$'\t'}"
  query="$(printf '%s\n' "$content" | awk '{print $1, $2, $3}')"
  if [[ -z "${query// }" ]]; then
    query="user preference"
  fi
  payload="$(jq -n --arg tenant_id "$tenant_id" --arg query "$query" --argjson top_k "$TOP_K" \
    '{tenant_id:$tenant_id,query:$query,top_k:$top_k}')"

  req_start_ns="$(now_ns)"
  http_code="$(curl -sS -o /dev/null -w '%{http_code}' \
    -X POST "$BASE_URL/v1/memory/search" \
    -H 'Content-Type: application/json' \
    --data "$payload")"
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

result_json="$OUT_DIR/${run_id}_${BACKEND}_${fixture_name%.json}.json"
summary_txt="$OUT_DIR/${run_id}_${BACKEND}_${fixture_name%.json}.summary.txt"

cat > "$result_json" <<EOF
{
  "timestamp_utc": "$timestamp_utc",
  "backend": "$BACKEND",
  "fixture": "$FIXTURE",
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
Timestamp (UTC): $timestamp_utc
Backend        : $BACKEND
Embedder       : $EMBEDDING_PROVIDER
Embed model    : $OLLAMA_MODEL
Fixture        : $FIXTURE
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
