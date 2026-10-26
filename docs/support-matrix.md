# Support Matrix

What each part of Pali is, how it is tested, and whether it is on by default.

Status meanings:

- **Supported**: on the release path and exercised by the unit, integration, or e2e suites that CI runs.
- **Supported (not live-tested)**: implemented and documented, but CI never runs it against the real service. Tests use a fake HTTP server, a mocked SQL driver, or unit tests of helper functions only. Verify against your own deployment before relying on it.
- **Experimental**: off by default, behaviour may change between releases, and benchmark results for it are mixed or missing.

## Core runtime

| Component | Status | Default | Notes |
|---|---|---|---|
| REST API (`pali serve`) | Supported | on | |
| MCP server over stdio (`pali mcp serve`) | Supported | n/a | |
| Dashboard | Supported | on | Assets are embedded in the binary. |
| Multi-tenancy | Supported | on | Isolation covered by `test/integration/multitenancy_isolation_test.go`. |
| JWT auth | Supported | off | `auth.enabled: true` plus `auth.jwt_secret`. |
| Async post-processing queue | Supported | on | `postprocess.enabled`. |

## Storage

| Component | Config | Status | Default |
|---|---|---|---|
| SQLite metadata + FTS5 | `database.sqlite_dsn` | Supported | on |
| SQLite vector store | `vector_backend: sqlite` | Supported | on |
| Qdrant | `vector_backend: qdrant` | Supported (not live-tested) | off |
| PostgreSQL + pgvector | `vector_backend: pgvector` | Supported (not live-tested) | off |
| SQLite entity facts | `entity_fact_backend: sqlite` | Supported | on |
| Neo4j entity facts | `entity_fact_backend: neo4j` | Experimental | off |

The SQLite vector store scans every stored vector for the tenant on each query (no ANN index). Vectors are stored as float32 blobs; on an Apple M1 Pro one query over 10,000 vectors takes about 27 ms at 256 dimensions and 82 ms at 768. Use Qdrant or pgvector for larger tenants.

## Embeddings

| Provider | Status | Default | Notes |
|---|---|---|---|
| `lexical` | Supported | on | Deterministic hashed bag-of-words vector. No model and no semantic matching: "car" and "automobile" do not match. Meant for first boot, CI, and smoke tests. |
| `ollama` | Supported (not live-tested) | off | Requires a running Ollama server and model. |
| `onnx` | Supported (not live-tested) | off | Requires the ONNX Runtime shared library. See [ONNX Runtime](onnx.md). |
| `openrouter` | Supported (not live-tested) | off | Requires `openrouter.api_key`. |

For semantic recall, switch `embedding.provider` from `lexical` to `ollama`, `onnx`, or `openrouter`.

## Importance scoring

| Scorer | Status | Default |
|---|---|---|
| `heuristic` | Supported | on |
| `ollama` | Supported (not live-tested) | off |
| `openrouter` | Supported (not live-tested) | off |

Importance has a weight of `0.1` under the default `wal` scoring, so the choice of scorer has little effect on ranking.

## Retrieval features

| Feature | Config | Status | Default |
|---|---|---|---|
| Hybrid lexical + vector candidates with RRF fusion | always on | Supported | on |
| `wal` / `match` scoring | `retrieval.scoring.algorithm` | Supported | `wal` |
| Query-type routing | `retrieval.answer_type_routing_enabled` | Supported | on |
| Early-rank rerank | `retrieval.early_rank_rerank_enabled` | Supported | on |
| Entity-fact bridge for multi-hop queries | `retrieval.multi_hop.entity_fact_bridge_enabled` | Experimental | on, but only finds something when entity facts exist |
| Adaptive query expansion | `retrieval.search.adaptive_query_expansion_enabled` | Experimental | off |
| LLM multi-hop query decomposition | `retrieval.multi_hop.llm_decomposition_enabled` | Experimental | off |
| Graph path retrieval | `retrieval.multi_hop.graph_path_enabled` | Experimental | off |

## Write-path enrichment

| Feature | Config | Status | Default |
|---|---|---|---|
| Fact parser (heuristic, Ollama, OpenRouter) | `parser.enabled` | Experimental | off |
| Structured observation / event dual-write | `structured_memory.enabled` | Experimental | off |
| Answer-span retention | `parser.answer_span_retention_enabled` | Experimental | off |
| Profile support links | `profile_layer.support_links_enabled` | Experimental | off |

The parser and entity-fact layer are the main area under active work. On the LoCoMo benchmark, enabling the LLM parser improves retrieval over raw turns, but the parser-plus-graph setup has scored lower than the parser alone in at least one run, and the cause is not yet known.

## SDKs

| SDK | Location |
|---|---|
| Go | `pkg/client` in this repo, see [Go Client SDK](client/README.md) |
| Python | [`pali-py`](https://github.com/pali-mem/pali-py) |
| JavaScript | [`pali-js`](https://github.com/pali-mem/pali-js) |
