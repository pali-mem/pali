# Architecture

Current implementation follows `repo.md` with an operational retrieval path.

## Embeddings

- `embedding.provider: ollama` (default): offline HTTP embedder via local Ollama server (`/api/embed`).
- `embedding.provider: onnx`: advanced local inference path; validates model/tokenizer paths and runs ONNX Runtime inference.
- `embedding.provider: openrouter`: remote embeddings via OpenRouter (`/api/v1/embeddings`).
- `embedding.provider: lexical` (legacy alias: `mock`): pure-Go lexical fallback provider.
- `embedding.fallback_provider: lexical` (default): used automatically when primary provider initialization fails.

## Retrieval Pipeline

Memory search uses hybrid retrieval + reranking:

1. **Candidate generation**
   - lexical BM25 ranking from SQLite FTS5 (`memory_fts`)
   - vector ranking from configured embedder + vectorstore when available
2. **Fusion**
   - Reciprocal Rank Fusion (RRF, `k=60`) merges lexical + dense ranks without score-scale tuning
3. **Configurable reranking** (`internal/core/memory/search.go`)
   - candidate window is clamped to **50..200** (`topK * 5`)
   - `retrieval.scoring.algorithm=wal`:
     `score = weighted(recency, relevance, importance)`
   - `retrieval.scoring.algorithm=match`:
     `score = weighted(recency, relevance, importance, query_overlap, route_fit)`
   - recency uses Ebbinghaus-style decay:
     `recency = 0.995 ^ hours_since_last_access`
   - feature signals are normalized to `[0,1]`
   - weights are config-driven from `pali.yaml`

After successful search, returned memories are `Touch`-updated (`last_accessed_at`, `last_recalled_at`, `recall_count`) for future recency/recall tracking.
