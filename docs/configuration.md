# Configuration Guide

This is the canonical config reference for Pali.

Source of truth (in order):
1. `internal/config/defaults.go`
2. `internal/config/config.go` + `internal/config/validation.go`
3. `pali.yaml.example` (must match defaults)

## Files and Flow

- `pali.yaml.example`: committed canonical config template.
- `pali.yaml`: local runtime config used by `make run` / `make mcp`.
- `cmd/setup`: copies `pali.yaml.example` to `pali.yaml` if missing.

Resolution order:
1. Code defaults
2. Values from YAML
3. Environment fallback keys:
   - `OPENROUTER_API_KEY` -> `openrouter.api_key` when YAML is empty
   - `NEO4J_PASSWORD` -> `neo4j.password` when YAML is empty

Environment fallbacks are intentionally limited today to:
- OpenRouter API key
- Neo4j password

For production secret management, prefer platform secret injection for the config file itself
(`auth.jwt_secret`, provider tokens, and any external API credentials) and keep
`pali.yaml` out of source control.

## Current Defaults

```yaml
server:
  host: 127.0.0.1
  port: 8080

vector_backend: sqlite                 # sqlite | qdrant | pgvector
entity_fact_backend: sqlite            # sqlite | neo4j
default_tenant_id: ""
importance_scorer: heuristic           # heuristic | ollama | openrouter

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
  scoring:
    algorithm: wal                     # wal | match
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

parser:
  enabled: false
  provider: heuristic                  # heuristic | ollama | openrouter
  ollama_base_url: http://127.0.0.1:11434
  ollama_model: deepseek-r1:7b
  openrouter_model: openai/gpt-oss-120b:nitro
  ollama_timeout_ms: 20000
  store_raw_turn: true
  max_facts: 4
  dedupe_threshold: 0.88
  update_threshold: 0.94

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
  password: ""                         # or set NEO4J_PASSWORD
  database: neo4j
  timeout_ms: 2000
  batch_size: 256

embedding:
  provider: ollama                     # ollama | onnx | lexical | openrouter | mock
  fallback_provider: lexical
  ollama_base_url: http://127.0.0.1:11434
  ollama_model: mxbai-embed-large
  ollama_timeout_seconds: 10
  model_path: ./models/all-MiniLM-L6-v2/model.onnx
  tokenizer_path: ./models/all-MiniLM-L6-v2/tokenizer.json

openrouter:
  base_url: https://openrouter.ai/api/v1
  api_key: ""                          # or set OPENROUTER_API_KEY
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

## Provider Test Profiles

Stable provider profiles for tests/evals:
- `test/config/providers/mock.yaml`
- `test/config/providers/lexical.yaml`
- `test/config/providers/ollama.yaml`
- `test/config/providers/qdrant-ollama.yaml`
- `test/config/providers/qdrant-neo4j-lexical.yaml`

They are consumed by:
- integration/e2e tests (mock profile)
- benchmark and retrieval scripts via `cmd/configrender`

## Neo4j Graph Edges

Neo4j edges are written from extracted entity facts, not from raw memory rows.

Requirements for relationship creation:
- `entity_fact_backend: neo4j`
- `parser.enabled: true` (or structured dual-write settings that emit parsed facts)

Graph shape written by the Neo4j repository:
- `(e:PaliEntity)-[:HAS_FACT]->(f:PaliEntityFact)`
- `(f:PaliEntityFact)-[:SOURCE_MEMORY]->(m:PaliMemory)` when `memory_id` exists

Quick verification queries:

```cypher
MATCH ()-[r]-() RETURN type(r), count(*) ORDER BY count(*) DESC;
```

```cypher
MATCH (e:PaliEntity)-[:HAS_FACT]->(f:PaliEntityFact)-[:SOURCE_MEMORY]->(m:PaliMemory)
RETURN e,f,m LIMIT 25;
```

## Rendering Config for Bench/Eval Runs

Use:

```bash
go run ./cmd/configrender \
  -profile test/config/providers/ollama.yaml \
  -out /tmp/pali.eval.yaml \
  -host 127.0.0.1 \
  -port 18080 \
  -vector-backend sqlite \
  -sqlite-dsn "file:/tmp/pali.eval.sqlite?cache=shared"
```

Then run:

```bash
go run ./cmd/pali -config /tmp/pali.eval.yaml
```

Neo4j graph-mode helpers in `cmd/configrender`:
- `-entity-fact-backend neo4j`
- `-parser-enabled true`
- `-parser-provider heuristic`

`scripts/benchmark.sh` and `scripts/retrieval_quality.sh` now auto-enable parser extraction when `--entity-fact-backend neo4j` is selected.
