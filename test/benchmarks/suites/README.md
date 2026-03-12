# Benchmark Suites

Config-driven suites for speed and retrieval benchmarking.

Run a suite:

```bash
python test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.local.json
```

Each run writes:

- `test/benchmarks/results/suites/<timestamp>-<suite>/suite.json`
- `test/benchmarks/results/suites/<timestamp>-<suite>/suite.summary.md`
- per-scenario logs under `logs/`

## Config Shape

```json
{
  "name": "speed-local",
  "out_root": "test/benchmarks/results/suites",
  "fail_fast": false,
  "common": {
    "benchmark": { "...": "flags for scripts/benchmark.sh" },
    "retrieval_quality": { "...": "flags for scripts/retrieval_quality.sh" },
    "locomo_qa": { "...": "flags for research/eval_locomo_f1_bleu.py" }
  },
  "scoring_defaults": {
    "latency_slo_ms": {
      "search": { "p50": 80, "p95": 120, "p99": 180 },
      "store": { "p95": 25 }
    },
    "throughput_targets_ops_sec": { "store": 20, "search": 7 },
    "weights": { "latency": 0.7, "throughput": 0.3 }
  },
  "scenarios": [
    {
      "id": "sqlite_lexical_speed",
      "runner": "benchmark",
      "enabled": true,
      "args": { "...": "runner-specific flags" }
    }
  ],
  "comparisons": [
    {
      "id": "lexical_vs_qdrant",
      "baseline": "sqlite_lexical_speed",
      "candidate": "qdrant_ollama_speed",
      "metrics": [
        "search_p95_ms",
        "search_ops_sec",
        "store_ops_sec",
        "scoring.performance_score"
      ]
    }
  ]
}
```

## Runner Types

- `benchmark` maps to `scripts/benchmark.sh`
- `retrieval_quality` maps to `scripts/retrieval_quality.sh`
- `locomo_qa` maps to `research/eval_locomo_f1_bleu.py`

## Included Suites

- `speed.local.json`: local SQLite + lexical smoke suite.
- `speed.medium.fast.json`: medium-size fast suite (500 fixture rows / 231 labeled eval queries).
- `speed.medium.qdrant-openrouter.json`: medium-size qdrant + OpenRouter lane (remote embedder; comparison reference).
- `speed.medium.qdrant-openrouter-parser-graph.json`: medium-size qdrant + OpenRouter lane with parser=openrouter and graph singleton invalidation enabled.
- `speed.qdrant_ollama.json`: lexical baseline vs qdrant+ollama, plus scorer-profile comparisons.
- `speed.locomo.optional.json`: optional LoCoMo lane template (disabled by default).
