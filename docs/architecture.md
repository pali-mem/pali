# Architecture

Current implementation follows the repository runtime behavior and the config surface described in `README.md` and `docs/configuration.md`.

## Infrastructure Shape

Pali keeps one core memory service and exposes it through multiple operator and application surfaces:

- REST API for application integration
- MCP server for agent hosts
- dashboard handlers for operator visibility

The repository layer remains the source of truth for persisted memory rows and tenant metadata. Optional systems such as Qdrant and Neo4j extend retrieval and entity-fact behavior rather than replacing the metadata store used by the dashboard and most service operations.

## Embeddings

- `embedding.provider: lexical` (default): zero-dependency lexical fallback provider for first boot, CI, and smoke runs.
- `embedding.provider: ollama`: offline HTTP embedder via local Ollama server (`/api/embed`).
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

## Dashboard and Operator Surface

The dashboard is built for operators inspecting the running service:

- tenant lists and counts come from the tenant and memory repositories
- persisted memory rows are rendered from the core repository-backed memory model
- retrieval-backed actions still use the memory service, so configured vector/entity extensions affect recall and ranking

Operationally, this means enabling Qdrant or Neo4j does not stop the dashboard from showing memories. Those backends enrich retrieval; they do not become the canonical listing store.

## Extension Boundaries

Current extension points are all config-driven:

- vector storage: `sqlite`, `qdrant`
- entity facts: `sqlite`, `neo4j`
- embeddings: `ollama`, `onnx`, `lexical`, `openrouter`
- importance scoring: `heuristic`, `ollama`, `openrouter`
- retrieval scoring: `wal`, `match`
- parsing and query decomposition: heuristic or LLM-backed providers where enabled

That keeps the application contract stable while letting operators change the retrieval stack underneath it.
