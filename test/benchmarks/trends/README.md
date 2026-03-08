# Retrieval Trend History

This folder stores retrieval-quality trend records as JSON Lines.

Primary file:
- `retrieval_quality_history.jsonl`

Append a record with:

```bash
scripts/retrieval_trend.sh \
  --label "curated-baseline" \
  --fixture test/fixtures/memories.json \
  --eval-set test/fixtures/retrieval_eval.curated.json \
  --top-k 5 --max-queries 0 \
  --embedding-provider ollama --embedding-model all-minilm
```

Each row includes commit hash, run label, fixture/eval config, and retrieval metrics.
