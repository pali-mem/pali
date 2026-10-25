# Architecture

Current implementation follows the repository runtime behavior and the config surface described in `README.md` and `docs/configuration.md`.

## Infrastructure Shape

Pali keeps one core memory service and exposes it through multiple operator and application surfaces:

- REST API for application integration
- MCP server for agent hosts
- dashboard handlers for operator visibility

The repository layer remains the source of truth for persisted memory rows and tenant metadata. Optional systems such as Qdrant and Neo4j extend retrieval and entity-fact behavior rather than replacing the metadata store used by the dashboard and most service operations.

## Embeddings

- `embedding.provider: lexical` (default, legacy alias `mock`): pure-Go hashed bag-of-words vector. Zero dependencies, for first boot, CI, and smoke runs. It does not match synonyms or paraphrases.
- `embedding.provider: ollama`: offline HTTP embedder via local Ollama server (`/api/embed`).
- `embedding.provider: onnx`: advanced local inference path; validates model/tokenizer paths and runs ONNX Runtime inference.
- `embedding.provider: openrouter`: remote embeddings via OpenRouter (`/api/v1/embeddings`).
- `embedding.fallback_provider: lexical` (default): used automatically when primary provider initialization fails.

## Memory Tiers

Tiers are `working`, `episodic`, and `semantic`; `auto` is accepted as input only and is never stored. Explicit non-`auto` tiers are kept. With `auto` (or an empty tier), the write path (`internal/core/memory/tier_policy.go`) stores `semantic` when any stable signal is present, otherwise `episodic`:

- `created_by` is `user` or `system`
- a semantic tag such as `preferences`, `profile`, `identity`, or `always`
- preference or profile phrasing in the content, such as `user prefers`, `i prefer`, or `my name is`

## Retrieval Pipeline

Memory search uses hybrid retrieval + reranking:

1. **Query planning**
   - the query is classified (temporal, person, multi-hop, and so on) and may be expanded into extra query variants
   - the candidate window is `topK * 5` (minimum 50), plus a boost for temporal, multi-hop, and filtered queries; it is capped at 200, or 320 for multi-hop and filtered queries (`retrieval.search.candidate_window_*`)
2. **Candidate generation**
   - lexical candidates from SQLite FTS5 (`memory_fts`), carrying the BM25 score normalized per query
   - vector candidates from the configured embedder + vector store
   - entity-fact candidates for multi-hop queries when entity facts exist
3. **Fusion**
   - Reciprocal Rank Fusion (RRF, `k=60`) merges lexical + dense ranks
4. **Configurable reranking** (`internal/core/memory/search_ranking_scoring.go`)
   - relevance is a weighted blend of dense score and rank, lexical score and rank, RRF score, entity match, and query-route fit
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

- vector storage: `sqlite`, `qdrant`, `pgvector`
- entity facts: `sqlite`, `neo4j`
- embeddings: `ollama`, `onnx`, `lexical`, `openrouter`
- importance scoring: `heuristic`, `ollama`, `openrouter`
- retrieval scoring: `wal`, `match`
- parsing and query decomposition: heuristic or LLM-backed providers where enabled

That keeps the application contract stable while letting operators change the retrieval stack underneath it.
