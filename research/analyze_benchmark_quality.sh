#!/usr/bin/env bash
# analyze_benchmark_quality.sh — compare two retrieval runs and score benchmark usefulness.
set -euo pipefail

cd "$(dirname "$0")/.."

OLLAMA_JSON=""
LEXICAL_JSON=""
OUT_JSON=""
OUT_MD=""
DELTA_THRESHOLD="0.03"
MIN_CASES="100"

usage() {
  cat <<'EOF'
Usage:
  research/analyze_benchmark_quality.sh \
    --ollama-json <path> \
    --lexical-json <path> \
    --out-json <path> \
    --out-md <path>

Notes:
  - This does not use LLM-as-a-judge.
  - It scores benchmark quality via:
    1) coverage (enough eval cases),
    2) curation (labeled eval-set vs auto-generated),
    3) discriminative power (ollama vs lexical metric gap).
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ollama-json)
      OLLAMA_JSON="$2"
      shift 2
      ;;
    --lexical-json)
      LEXICAL_JSON="$2"
      shift 2
      ;;
    --out-json)
      OUT_JSON="$2"
      shift 2
      ;;
    --out-md)
      OUT_MD="$2"
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

require_file "$OLLAMA_JSON"
require_file "$LEXICAL_JSON"

if [[ -z "$OUT_JSON" || -z "$OUT_MD" ]]; then
  echo "ERROR: --out-json and --out-md are required"
  exit 1
fi

mkdir -p "$(dirname "$OUT_JSON")" "$(dirname "$OUT_MD")"

metric() {
  local file="$1"
  local name="$2"
  jq -r ".metrics.${name}" "$file"
}

absf() {
  awk -v x="$1" 'BEGIN{if (x < 0) x = -x; printf "%.6f", x}'
}

subf() {
  awk -v a="$1" -v b="$2" 'BEGIN{printf "%.6f", a-b}'
}

geqf() {
  awk -v a="$1" -v b="$2" 'BEGIN{print (a >= b) ? "1" : "0"}'
}

top1_o="$(metric "$OLLAMA_JSON" "top1_hit_rate")"
recall_o="$(metric "$OLLAMA_JSON" "recall_at_k")"
ndcg_o="$(metric "$OLLAMA_JSON" "ndcg_at_k")"
mrr_o="$(metric "$OLLAMA_JSON" "mrr")"

top1_l="$(metric "$LEXICAL_JSON" "top1_hit_rate")"
recall_l="$(metric "$LEXICAL_JSON" "recall_at_k")"
ndcg_l="$(metric "$LEXICAL_JSON" "ndcg_at_k")"
mrr_l="$(metric "$LEXICAL_JSON" "mrr")"

delta_top1="$(subf "$top1_o" "$top1_l")"
delta_recall="$(subf "$recall_o" "$recall_l")"
delta_ndcg="$(subf "$ndcg_o" "$ndcg_l")"
delta_mrr="$(subf "$mrr_o" "$mrr_l")"

abs_delta_top1="$(absf "$delta_top1")"
abs_delta_recall="$(absf "$delta_recall")"
abs_delta_ndcg="$(absf "$delta_ndcg")"
abs_delta_mrr="$(absf "$delta_mrr")"

eval_cases_selected="$(jq -r '.eval_cases_selected' "$OLLAMA_JSON")"
eval_set_path="$(jq -r '.eval_set // ""' "$OLLAMA_JSON")"
query_words="$(jq -r '.query_words // 0' "$OLLAMA_JSON")"

coverage_ok="$(geqf "$eval_cases_selected" "$MIN_CASES")"
curated_eval_set="0"
if [[ -n "$eval_set_path" ]]; then
  curated_eval_set="1"
fi

discriminative_ok="0"
if [[ "$(geqf "$abs_delta_top1" "$DELTA_THRESHOLD")" == "1" || \
      "$(geqf "$abs_delta_ndcg" "$DELTA_THRESHOLD")" == "1" || \
      "$(geqf "$abs_delta_mrr" "$DELTA_THRESHOLD")" == "1" ]]; then
  discriminative_ok="1"
fi

leakage_risk="low"
if [[ "$curated_eval_set" == "0" && "$query_words" -le 3 ]]; then
  leakage_risk="high"
fi

overall="needs_improvement"
if [[ "$coverage_ok" == "1" && "$curated_eval_set" == "1" && "$discriminative_ok" == "1" && "$leakage_risk" == "low" ]]; then
  overall="good_for_regression_tracking"
fi

cat > "$OUT_JSON" <<EOF
{
  "overall_assessment": "$overall",
  "thresholds": {
    "min_eval_cases": $MIN_CASES,
    "min_abs_metric_gap_for_discrimination": $DELTA_THRESHOLD
  },
  "checks": {
    "coverage_ok": $coverage_ok,
    "curated_eval_set": $curated_eval_set,
    "discriminative_ok": $discriminative_ok,
    "query_leakage_risk": "$leakage_risk"
  },
  "providers": {
    "ollama": {
      "top1_hit_rate": $top1_o,
      "recall_at_k": $recall_o,
      "ndcg_at_k": $ndcg_o,
      "mrr": $mrr_o
    },
    "lexical": {
      "top1_hit_rate": $top1_l,
      "recall_at_k": $recall_l,
      "ndcg_at_k": $ndcg_l,
      "mrr": $mrr_l
    }
  },
  "delta_ollama_minus_lexical": {
    "top1_hit_rate": $delta_top1,
    "recall_at_k": $delta_recall,
    "ndcg_at_k": $delta_ndcg,
    "mrr": $delta_mrr
  },
  "inputs": {
    "ollama_result_json": "$OLLAMA_JSON",
    "lexical_result_json": "$LEXICAL_JSON",
    "eval_cases_selected": $eval_cases_selected,
    "eval_set": "$eval_set_path"
  }
}
EOF

cat > "$OUT_MD" <<EOF
# Research: Benchmark Quality Check

## Summary

- overall assessment: **$overall**
- eval cases selected: **$eval_cases_selected**
- curated eval set: **$curated_eval_set**
- discriminative check (ollama vs lexical): **$discriminative_ok**
- query leakage risk: **$leakage_risk**

## Retrieval Metrics (No LLM Judge)

| Metric | Ollama | Lexical | Delta (Ollama - Lexical) |
|---|---:|---:|---:|
| Top1HitRate | $top1_o | $top1_l | $delta_top1 |
| Recall@K | $recall_o | $recall_l | $delta_recall |
| nDCG@K | $ndcg_o | $ndcg_l | $delta_ndcg |
| MRR | $mrr_o | $mrr_l | $delta_mrr |

## Input Artifacts

- ollama: \`$OLLAMA_JSON\`
- lexical: \`$LEXICAL_JSON\`
- eval_set: \`${eval_set_path:-"(auto-generated)"}\`
EOF

echo "Wrote:"
echo "  $OUT_JSON"
echo "  $OUT_MD"
