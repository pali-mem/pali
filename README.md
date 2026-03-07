# Pali

Persistent memory layer for LLM applications.

v0.1 currently ships a working local server with REST + MCP + dashboard on SQLite.

## Current Status

Implemented and tested now:
- `GET /` redirects to `/dashboard`
- `GET /health` returns status
- `GET /dashboard` renders a simple web dashboard
- `POST /v1/tenants` creates tenant records
- `POST /v1/memory` / `POST /v1/memory/batch` / `POST /v1/memory/search` / `DELETE /v1/memory/:id` are wired to core+sqlite
- `GET /v1/tenants/:id/stats` returns tenant memory counts
- Retrieval uses two-phase search: lexical + dense candidate fusion (RRF) followed by WMR reranking
- `tier=auto` is resolved at store time to `episodic` or `semantic` using deterministic signals
- Config-driven importance scorer selection (`heuristic` default, `ollama` optional)
- Config-driven vector backend selection (`sqlite`, `qdrant` implemented; `pgvector` currently fail-fast placeholder)
- Dashboard pages for tenants and memories are functional (list/create/search/delete flows)

## Repository Layout

Key top-level paths:
- `cmd/pali`: main API server binary
- `cmd/setup`: setup bootstrap command
- `internal/domain`: entities + interfaces
- `internal/core`: use-case/service layer
- `internal/repository/sqlite`: SQLite repository implementation
- `internal/vectorstore`: sqlite-vec + qdrant implementations, pgvector placeholder
- `internal/embeddings`: embedding provider implementations (ollama/onnx/lexical)
- `internal/scorer`: heuristic and ollama importance scorers
- `internal/api`: Gin router, middleware, handlers, DTOs
- `internal/mcp`: MCP server + tool handlers
- `internal/dashboard`: dashboard handlers + templates
- `internal/config`: config defaults/validation/loading
- `pkg/client`: Go API client SDK for `/health` and `/v1/*` endpoints
- `test`: integration/e2e fixtures + test utilities
- `docs`: architecture/API/MCP/deployment docs

## Prerequisites

- Go 1.24+

## Setup

1. Initialize local config + dependency checks:

```bash
make setup
```

2. Start API server:

```bash
make run
```

Optional MCP server over stdio:

```bash
make mcp
# or directly:
go run ./cmd/pali mcp run -config pali.yaml.example
```

MCP toolset currently exposes 11 common operations:
- `memory_store`
- `memory_store_preference`
- `memory_search`
- `memory_list`
- `memory_delete`
- `tenant_create`
- `tenant_list`
- `tenant_stats`
- `tenant_exists`
- `health_check`
- `pali_capabilities`

Pali also exposes built-in MCP guidance for better default adoption:
- `initialize.instructions` with memory-first policy hints
- `prompts/get` prompt: `pali_memory_autopilot`

For tenant-aware MCP tools, `tenant_id` is optional when a fallback is available. Resolution order:
1. `tenant_id` in tool input
2. JWT tenant claim (if auth is enabled and provided by transport)
3. MCP session default tenant
4. `default_tenant_id` in config
5. otherwise the tool returns an error

Server default address: `http://127.0.0.1:8080`

Health check:

```bash
curl http://127.0.0.1:8080/health
```

Home route:

```bash
curl http://127.0.0.1:8080/
```

Dashboard:

```bash
open http://127.0.0.1:8080/dashboard
```

### Embedding setup behavior

`make setup` (and `go run ./cmd/setup`) checks your configured embedder and only downloads ONNX model files when `embedding.provider=onnx`.
To force ONNX model prefetch for advanced usage, run:

```bash
go run ./cmd/setup -download-model
```

To always skip model download, use:

```bash
go run ./cmd/setup -skip-model-download
```

`cmd/setup` also checks ONNX Runtime shared library availability and prints install hints when missing.
Skip that check with:

```bash
go run ./cmd/setup -skip-runtime-check
```

`cmd/setup` checks Ollama server + model readiness by default (`/api/version`, `/api/tags`).
Skip that check with:

```bash
go run ./cmd/setup -skip-ollama-check
```

This fetches:

- `models/all-MiniLM-L6-v2/model.onnx`
- `models/all-MiniLM-L6-v2/tokenizer.json`

Without these files, selecting `embedding.provider: onnx` will fail at startup.

Embedding provider selection (in `pali.yaml`):

```yaml
default_tenant_id: default
vector_backend: sqlite
importance_scorer: heuristic # heuristic | ollama
structured_memory:
  enabled: false
  dual_write_observations: false
  dual_write_events: false
  query_routing_enabled: false
  max_observations: 3

retrieval:
  scoring:
    algorithm: wal # wal | match
    wal:
      recency: 1.0
      relevance: 1.0
      importance: 1.0
    match:
      recency: 0.05
      relevance: 0.70
      importance: 0.10
      query_overlap: 0.10
      routing: 0.05

parser:
  enabled: false
  provider: heuristic # heuristic | ollama
  ollama_base_url: http://127.0.0.1:11434
  ollama_model: qwen2.5:7b
  ollama_timeout_ms: 20000
  store_raw_turn: true
  max_facts: 4
  dedupe_threshold: 0.88
  update_threshold: 0.94

qdrant:
  base_url: http://127.0.0.1:6333
  api_key: ""
  collection: pali_memories
  timeout_ms: 2000

embedding:
  provider: ollama # ollama | onnx | lexical (mock alias supported)
  fallback_provider: "" # optional explicit fallback, e.g. lexical
  ollama_base_url: http://127.0.0.1:11434
  ollama_model: mxbai-embed-large
  ollama_timeout_seconds: 10

ollama:
  base_url: http://127.0.0.1:11434
  model: qwen2.5:7b
  timeout_ms: 2000
```

Current behavior:

- `ollama` is the default embedding provider (requires local Ollama server).
- `fallback_provider` is opt-in; leave it empty to fail fast when the primary provider is unavailable.
- `lexical` is the pure-Go local fallback provider (legacy `mock` alias still works).
- `importance_scorer` controls importance scoring (`heuristic` default, `ollama` opt-in).
- `retrieval.scoring.algorithm` switches reranking mode (`wal` default, `match` for QA-heavy relevance-first behavior).
- `parser.enabled` enables an extraction stage before write-time persistence to reduce raw-turn noise.
- `structured_memory.enabled` + `dual_write_observations` can dual-write concise derived observation memories at store time.
- `structured_memory.dual_write_events` can derive time-anchored event memories from annotated turns.
- `structured_memory.query_routing_enabled` applies light query-intent boosts (temporal/person/multi-hop) during ranking.
- retrieval uses hybrid fusion: lexical ranking + dense vector ranking merged with Reciprocal Rank Fusion (RRF, `k=60`) before WMR reranking.
- `vector_backend=sqlite` and `vector_backend=qdrant` are implemented in v0.1.
- `vector_backend=pgvector` currently returns a startup error until the adapter is completed.
- `tier=auto` resolves to:
  - `semantic` when preference/profile signals are present (`created_by=user|system`, semantic tags, or preference-like phrases)
  - otherwise `episodic`

Ollama quick start:

```bash
# install: https://ollama.com/download
ollama serve
ollama pull mxbai-embed-large
```

Set runtime library path when needed:

```bash
export ONNXRUNTIME_SHARED_LIBRARY_PATH=/path/to/libonnxruntime.dylib   # macOS
export ONNXRUNTIME_SHARED_LIBRARY_PATH=/path/to/libonnxruntime.so      # Linux
```

Install notes:
- macOS: `brew install onnxruntime` typically provides `/opt/homebrew/lib/libonnxruntime.dylib`
- Windows: use ONNX Runtime release zip for `onnxruntime.dll` and install Microsoft Visual C++ runtime
- Full advanced setup (macOS + Windows + Linux): `docs/onnx.md`

JWT auth (optional, per-tenant):

```yaml
auth:
  enabled: true
  jwt_secret: "change-me"
  issuer: "pali"
```

JWT must include `tenant_id`; request tenant must match token tenant.

Mint a dev JWT quickly:

```bash
# Uses auth.jwt_secret and auth.issuer from pali.yaml when present
go run ./cmd/jwt -tenant tenant_1

# Explicit secret + ttl
go run ./cmd/jwt -tenant tenant_1 -secret "change-me" -ttl 2h

# Makefile shortcut
TENANT=tenant_1 JWT_SECRET=change-me make jwt
```

## Go Client (`pkg/client`)

```go
import (
	"context"
	"log"

	"github.com/pali-mem/pali/pkg/client"
)

func main() {
	c, err := client.NewClient("http://127.0.0.1:8080")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if _, err := c.CreateTenant(ctx, client.CreateTenantRequest{
		ID:   "tenant_1",
		Name: "Tenant One",
	}); err != nil {
		log.Fatal(err)
	}
}
```

When auth is enabled, set bearer token once:

```go
c.SetBearerToken("<jwt>")
```

Client documentation:
- `docs/client/README.md`

## Build and Test

```bash
make build
```

Tests are split by scope using build tags:

| Command | Scope |
|---|---|
| `make test` | Unit tests only — `internal/` and `pkg/`, always fast |
| `make test-integration` | Integration tests — requires real DB (`-tags integration`) |
| `make test-e2e` | End-to-end tests (`-tags e2e`, self-contained/in-process) |
| `make test-all` | Everything |

Release smoke gate:

```bash
make test && make test-integration && make test-e2e && make build
```

```bash
make test
make test-integration
make test-e2e
make test-all
```

Integration and e2e test files carry a `//go:build integration` or `//go:build e2e` tag at the top — they are not compiled at all unless the matching tag is passed.

## Benchmarks

```bash
make bench-setup
make benchmark
make retrieval-quality
```

Or run directly with flags:

```bash
scripts/gen_fixtures.sh --model phi4-mini --count 1000 --tenants 100 --parallel 8 --out test/fixtures/memories_real.json
scripts/benchmark.sh --fixture test/fixtures/memories_real.json --backend sqlite --search-ops 500 --top-k 10 --embedding-provider ollama --embedding-model all-minilm
scripts/retrieval_quality.sh --fixture test/fixtures/memories_real.json --top-k 10 --max-queries 200 --embedding-provider ollama --embedding-model all-minilm
scripts/retrieval_quality.sh --fixture test/fixtures/memories_real.json --eval-set test/fixtures/retrieval_eval.sample.json --top-k 10 --embedding-provider ollama --embedding-model all-minilm
scripts/retrieval_quality.sh --fixture test/fixtures/memories.json --eval-set test/fixtures/retrieval_eval.curated.json --top-k 10 --max-queries 0 --embedding-provider ollama --embedding-model all-minilm
scripts/retrieval_trend.sh --label "curated-eval-run" --fixture test/fixtures/memories.json --eval-set test/fixtures/retrieval_eval.curated.json --top-k 10 --max-queries 0 --embedding-provider ollama --embedding-model all-minilm
```

Results are written to `test/benchmarks/results/` as JSON and text summary files.

## Database Docs

- SQLite implementation notes: `docs/db/sqlite.md`
- Performance/behavior change records: `docs/changes/`
- MCP integration notes + testing checklist: `docs/mcp.md`
- ONNX advanced setup notes: `docs/onnx.md`
- Go client SDK usage + structure: `docs/client/README.md`

## Important Next Steps

1. Curate and grow retrieval eval coverage over real workloads and tougher negatives.
2. Add dashboard pagination, richer filters, and inline edit flows.
3. Expand tenant stats with tier/tag/recall breakdowns.

## Module Path

`go.mod` is initialized as:

`github.com/pali-mem/pali`

---

See [ACKNOWLEDGEMENTS.md](ACKNOWLEDGEMENTS.md) for research papers, models, and open-source dependencies.
