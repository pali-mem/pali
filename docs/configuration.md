# Configuration Guide

This is the canonical config reference for Pali.

Source of truth, in order:

1. `internal/config/defaults.go`
2. `internal/config/config.go`
3. `internal/config/validation.go`
4. `pali.yaml.example`

## Config Files

- `pali.yaml.example`: committed canonical template
- `pali.yaml`: local default runtime file
- custom config path: supported everywhere via `-config`

The committed default config is intentionally zero-dependency:

- `vector_backend: sqlite`
- `entity_fact_backend: sqlite`
- `embedding.provider: lexical`

That makes first boot easy on any machine. It is not the highest-quality retrieval setup. For better semantic recall and ranking, move to `ollama`, `onnx`, or `openrouter` once the basic deployment is working.

`cmd/setup` will create the target config file from `pali.yaml.example` when it is missing:

```bash
go run ./cmd/setup -config /etc/pali/pali.yaml
```

## Resolution Order

1. code defaults
2. YAML values
3. limited environment fallbacks

Current environment fallbacks:

- `OPENROUTER_API_KEY` -> `openrouter.api_key`
- `NEO4J_PASSWORD` -> `neo4j.password`

## Current Defaults

```yaml
server:
  host: 127.0.0.1
  port: 8080

vector_backend: sqlite
entity_fact_backend: sqlite
default_tenant_id: ""
importance_scorer: heuristic

postprocess:
  enabled: true
  poll_interval_ms: 250
  batch_size: 32
  worker_count: 2
  lease_ms: 30000
  max_attempts: 5
  retry_base_ms: 500
  retry_max_ms: 60000

structured_memory:
  enabled: false
  dual_write_observations: false
  dual_write_events: false
  max_observations: 3

retrieval:
  answer_type_routing_enabled: false
  early_rank_rerank_enabled: false
  temporal_resolver_enabled: false
  open_domain_alternative_resolver_enabled: false
  scoring:
    algorithm: wal
    wal:
      recency: 0.1
      relevance: 0.8
      importance: 0.1
    match:
      recency: 0.05
      relevance: 0.70
      importance: 0.10
      query_overlap: 0.10
      routing: 0.05
  multi_hop:
    entity_fact_bridge_enabled: true
    llm_decomposition_enabled: false
    decomposition_provider: openrouter
    openrouter_model: openai/gpt-oss-120b:nitro
    ollama_base_url: http://127.0.0.1:11434
    ollama_model: deepseek-r1:7b
    ollama_timeout_ms: 2000
    max_decomposition_queries: 3
    enable_pairwise_rerank: true
    token_expansion_fallback: true

parser:
  enabled: false
  provider: heuristic
  ollama_base_url: http://127.0.0.1:11434
  ollama_model: deepseek-r1:7b
  openrouter_model: openai/gpt-oss-120b:nitro
  ollama_timeout_ms: 20000
  store_raw_turn: true
  max_facts: 4
  dedupe_threshold: 0.88
  update_threshold: 0.94
  answer_span_retention_enabled: false

profile_layer:
  support_links_enabled: false

database:
  sqlite_dsn: file:pali.db?cache=shared

qdrant:
  base_url: http://127.0.0.1:6333
  api_key: ""
  collection: pali_memories
  timeout_ms: 2000

neo4j:
  uri: bolt://127.0.0.1:7687
  username: neo4j
  password: ""
  database: neo4j
  timeout_ms: 2000
  batch_size: 256

embedding:
  provider: lexical
  fallback_provider: lexical
  ollama_base_url: http://127.0.0.1:11434
  ollama_model: mxbai-embed-large
  ollama_timeout_seconds: 10
  model_path: ./models/all-MiniLM-L6-v2/model.onnx
  tokenizer_path: ./models/all-MiniLM-L6-v2/tokenizer.json

openrouter:
  base_url: https://openrouter.ai/api/v1
  api_key: ""
  embedding_model: openai/text-embedding-3-small:nitro
  scoring_model: openai/gpt-oss-120b:nitro
  timeout_ms: 10000

ollama:
  base_url: http://127.0.0.1:11434
  model: deepseek-r1:7b
  timeout_ms: 2000

auth:
  enabled: false
  jwt_secret: ""
  issuer: pali

logging:
  dev_verbose: false
  progress: true
```

## Important Runtime Notes

- `vector_backend: sqlite` is implemented.
- `vector_backend: qdrant` is implemented.
- `entity_fact_backend: sqlite` and `entity_fact_backend: neo4j` are implemented.
- `embedding.provider: lexical` is the default because it requires no external services.
- `embedding.provider: lexical` is appropriate for CI, smoke tests, and local no-model runs.
- `embedding.provider: lexical` is not the best retrieval quality option; move to `ollama`, `onnx`, or `openrouter` when you want stronger semantic search.
- `embedding.provider: onnx` requires both model files and an ONNX Runtime shared library.
- `embedding.provider: openrouter` requires `openrouter.api_key`.
- `retrieval.multi_hop.llm_decomposition_enabled` is off by default.
- `retrieval.answer_type_routing_enabled` keeps single-hop, temporal, and open-domain routing changes additive until validated.
- `retrieval.early_rank_rerank_enabled` is intended to lift relevant hits from ranks `11-25` into `1-10` before increasing retrieval depth.
- `retrieval.temporal_resolver_enabled` is the gated path for stronger temporal answer normalization.
- `retrieval.open_domain_alternative_resolver_enabled` is the gated path for deterministic open-domain label/choice resolution.
- `parser.answer_span_retention_enabled` stores extra answer-bearing metadata on parsed memories without replacing existing canonical memory content.
- `profile_layer.support_links_enabled` stores source-support lines on summary/profile memories so retrieval can surface the summary and its backing evidence together.
- `retrieval.multi_hop.decomposition_provider: none` is only valid when LLM decomposition is disabled.

## Category Improvement Rollout

These flags were added for the single-hop / temporal / open-domain improvement slice.

- Leave them `false` for baseline and benchmark-comparable runs.
- Enable them together for category-focused experiments:
  - `retrieval.answer_type_routing_enabled: true`
  - `retrieval.early_rank_rerank_enabled: true`
  - `retrieval.temporal_resolver_enabled: true`
  - `retrieval.open_domain_alternative_resolver_enabled: true`
  - `parser.answer_span_retention_enabled: true`
  - `profile_layer.support_links_enabled: true`
- Roll back by flipping those flags back to `false`; the schema changes are additive and existing memories remain readable.

## Benchmark and Test Profiles

Provider base profiles live under `test/config/providers/`.

Benchmark entrypoints live under `test/benchmarks/profiles/`.

The benchmark scripts render a runtime config from the provider profile, then copy both into each result directory:

- `config.profile.yaml`
- `config.rendered.yaml`

That is the canonical record of benchmark configuration.

## Rendering a Config for Tests or Benchmarks

```bash
go run ./cmd/configrender \
  -profile test/config/providers/mock.yaml \
  -out /tmp/pali.eval.yaml \
  -host 127.0.0.1 \
  -port 18080 \
  -vector-backend sqlite \
  -sqlite-dsn "file:/tmp/pali.eval.sqlite?cache=shared"
```

Run it with:

```bash
go run ./cmd/pali -config /tmp/pali.eval.yaml
```

## Setup Command

Safe local bootstrap:

```bash
go run ./cmd/setup -config pali.yaml
```

Useful flags:

- `-skip-model-download`
- `-download-model`
- `-skip-runtime-check`
- `-skip-ollama-check`
- `-ollama-base-url`
- `-ollama-model`
- `-model-id`

## Validation Rules Worth Remembering

- `postprocess.*` timing and batch fields must be positive
- `parser.max_facts` must be positive
- parser thresholds must stay in `[0,1]`
- `structured_memory.max_observations` must be positive when dual-write modes are enabled
- OpenRouter settings are required when OpenRouter-backed embedding, parsing, or scoring is enabled
- Neo4j password is required when `entity_fact_backend: neo4j`
