# Pali

**Persistent Memory Layer for LLM Applications**

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8.svg)](https://go.dev/)
[![Status](https://img.shields.io/badge/status-beta-yellow.svg)]()

Self-hosted memory system that enables LLMs to persistently remember facts, preferences, and context across conversations. Designed for production deployments with offline-first architecture and zero external dependencies.

**Key Characteristics:**
- Single binary deployment
- Embedded vector search (sqlite-vec default, Qdrant/pgvector optional)
- Multi-tenant
- Native MCP server for tool integration
- Fully local operation — no cloud dependencies

---

## Overview

Pali provides memory layer for AI applications. Any LLM client (Claude Desktop, GPT, DeepSeek, Ollama, Open WebUI) can use Pali to maintain stateful context across sessions through a REST API or MCP protocol.

**Architecture:**
- Written in Go for performance and single-binary distribution
- SQLite for metadata and default vector storage
- Optional backends (Qdrant, pgvector) for scale
- ONNX Runtime for local embeddings (all-MiniLM-L6-v2)
- Built-in HTMX dashboard for memory management

**Runtime Dependency:** The default offline configuration requires ONNX Runtime shared library for embedding generation. See [Installation](#installation) for setup instructions.

---

## Quick Start

```bash
# Download and run setup (installs ONNX Runtime, downloads model)
pali setup

# Start server with default configuration
pali serve --config pali.yaml

# Server runs at http://localhost:8080
# Dashboard available at http://localhost:8080/dashboard
```

See [full installation guide](#installation) below.

---

## Use Cases

| Problem | Current Reality | Pali |
|---|---|---|
| LLMs forget everything between sessions | You repeat yourself every conversation | Persistent memory across sessions |
| "Self-hosted" often means multi-service stacks | You end up managing Docker + databases | Single binary, no external services by default |
| Most tools assume one user per instance | Multi-user apps become awkward fast | Multi-tenant by design |
| Memory tools are black boxes | Hard to inspect or edit what the AI remembers | Built-in dashboard to browse and manage |
| No MCP support | Can't integrate with Claude Code, Cursor, etc. | Native MCP server included |
| Scaling forces a full rewrite | You're locked into one vector backend | Pluggable backend — swap without touching app code |

---

## Core Features

### Memory Architecture

Pali implements a three-tier memory hierarchy:

1. **Working Memory** — Recent conversation exchanges injected verbatim (last N turns)
2. **Episodic Memory** — Time-bounded events and transient context with automatic decay
3. **Semantic Memory** — Long-term facts and preferences with no time-based decay

### Retrieval Engine

Memory retrieval uses **Weighted Memory Retrieval (WMR)** scoring, combining research from Park et al. (2023) *Generative Agents* and Zhong et al. (2024) *MemoryBank*:

**Scoring formula:**

```
score = w_recency × recency(t) + w_relevance × similarity(q,m) + w_importance × importance(m)

where:
  recency(t)   = decay_factor ^ hours_since_access  (Ebbinghaus curve)
  similarity   = cosine_similarity(embed(q), embed(m))
  importance   = TF-IDF + heuristics (or Ollama LLM scoring)
  
All factors normalized to [0,1] via min-max scaling
Default weights: 1.0 each (tunable per-tenant)
```

**Two-phase retrieval:**
1. Vector search narrows to top K candidates (50-200)
2. Full WMR scoring ranks final results

This architecture maintains consistent latency while leveraging vector backend performance..

### Multi-Backend Support

| Backend | Embedded | ANN Index | Multi-Tenant | Best For |
|---|:---:|:---:|:---:|---|
| **sqlite-vec** | ✅ | KNN only | Partition-based | Personal use, small teams, zero setup |
| **Qdrant** | ❌ | HNSW | Collection-based | 100k+ memories, production scale |
| **pgvector** | ❌ | HNSW | Schema-based | Existing PostgreSQL infrastructure |

Backend selection is configuration-only — no code changes required.

---

## Memory Policies

### Automatic Tier Assignment

When storing memories with `tier: "auto"`, Pali classifies content based on stability indicators:

**Semantic tier** (no decay):
- Explicit preferences and durable facts
- User or system-marked importance
- Tagged with `preferences`, `profile`, or `always`

**Episodic tier** (time-bounded):
- Event-based context
- Session-specific information
- Transient plans or constraints

**Working memory** operates independently — last N exchanges injected verbatim without retrieval scoring.

Manual tier assignment via API or dashboard always takes precedence.

### Deduplication

Pali implements per-tenant deduplication:
- Content hash matching for exact duplicates
- Optional key-based replacement for preference-type memories (e.g., `pref.language`)

### Conflict Handling

Conflicting memories are preserved, not silently deleted. When semantic facts disagree:
- Both memories remain accessible
- WMR scoring determines retrieval priority
- Dashboard highlights potential conflicts for manual review

This approach maintains audit trail and allows inspection of memory evolution.

---

## Installation

### Prerequisites

- Go 1.21+ (for building from source)
- ONNX Runtime 1.14+ (for embeddings)

### Binary Installation

Download the latest release for your platform:

```bash
# Linux/macOS
curl -L https://github.com/yourusername/pali/releases/latest/download/pali-$(uname -s)-$(uname -m) -o pali
chmod +x pali
sudo mv pali /usr/local/bin/

# Windows (PowerShell)
Invoke-WebRequest -Uri "https://github.com/yourusername/pali/releases/latest/download/pali-windows-amd64.exe" -OutFile "pali.exe"
```

### Setup

Run the setup wizard to install ONNX Runtime and download model files:

```bash
pali setup
```

This will:
- Detect your operating system
- Install or verify ONNX Runtime
- Download all-MiniLM-L6-v2 model and tokenizer
- Verify checksums
- Test embedding generation

### Configuration

Create `pali.yaml` (or copy from `pali.yaml.example`):

```yaml
server:
  host: localhost
  port: 8080

vector_backend: sqlite  # sqlite | qdrant | pgvector

embedding:
  model_path: ./models/all-MiniLM-L6-v2/model.onnx
  tokenizer_path: ./models/all-MiniLM-L6-v2/tokenizer.json

auth:
  enabled: false  # Enable for multi-tenant production

# Optional: Qdrant backend
# qdrant:
#   host: localhost
#   port: 6334

# Optional: pgvector backend
# pgvector:
#   dsn: postgres://user:pass@localhost:5432/pali
```

### Running

```bash
# Start server
pali serve --config pali.yaml

# Run in background
pali serve --config pali.yaml --daemon

# Check status
curl http://localhost:8080/health
```

---

## API Reference

### Store Memory

```http
POST /v1/memory
Content-Type: application/json
Authorization: Bearer <token>  # if auth enabled

{
  "tenant_id": "user_123",
  "content": "User prefers Go over Python for systems programming",
  "tags": ["preferences", "languages"],
  "tier": "auto"
}
```

**Response:**
```json
{
  "id": "mem_abc123",
  "created_at": "2026-03-03T10:00:00Z"
}
```

### Search Memories

```http
POST /v1/memory/search
Content-Type: application/json
Authorization: Bearer <token>

{
  "tenant_id": "user_123",
  "query": "What programming languages does the user prefer?",
  "top_k": 5,
  "min_score": 0.3,
  "tiers": ["episodic", "semantic"]
}
```

**Response:**
```json
{
  "results": [
    {
      "id": "mem_abc123",
      "content": "User prefers Go over Python for systems programming",
      "score": 0.87,
      "tier": "semantic",
      "tags": ["preferences", "languages"],
      "created_at": "2026-03-03T10:00:00Z",
      "last_recalled_at": "2026-03-03T12:30:00Z",
      "recall_count": 5
    }
  ]
}
```

### Delete Memory

```http
DELETE /v1/memory/:id?tenant_id=user_123
Authorization: Bearer <token>
```

### Tenant Statistics

```http
GET /v1/tenants/:id/stats
Authorization: Bearer <token>
```

**Response:**
```json
{
  "counts": {
    "working": 10,
    "episodic": 143,
    "semantic": 57
  },
  "top_tags": [
    {"tag": "preferences", "count": 23},
    {"tag": "work", "count": 18}
  ],
  "recall": {
    "total_recalls": 1247,
    "last_recalled_at": "2026-03-03T12:30:00Z"
  }
}
```

See full [API documentation](docs/api.md) for additional endpoints and parameters.

---

## MCP Integration

Pali provides native MCP (Model Context Protocol) server for integration with AI tools.

### Available Tools

| Tool | Description |
|---|---|
| `memory.store` | Store a new memory with automatic or explicit tier assignment |
| `memory.search` | Retrieve relevant memories for a query |
| `memory.delete` | Remove a specific memory |
| `tenant.stats` | Get memory statistics for a tenant |

### Configuration

Add to your MCP client configuration (e.g., Claude Desktop):

```json
{
  "mcpServers": {
    "pali": {
      "command": "pali",
      "args": ["mcp", "--config", "/path/to/pali.yaml"],
      "env": {
        "PALI_TOKEN": "your-bearer-token"
      }
    }
  }
}
```

See [MCP integration guide](docs/mcp.md) for detailed setup instructions.

---

## Architecture

### System Design

```text
┌──────────────────────────────────────────────────────────────┐
│                    LLM Client / App / Agent                   │
│          Claude / GPT / DeepSeek / Ollama / Open WebUI        │
│         calls Pali before each prompt (retrieve + store)      │
└──────────────├────────────────────────────├───────────────────
               │  HTTP REST (localhost:8080)│  MCP (stdio)
               ▼                           ▼
┌──────────────────────────────────────────────────────────────┐
│                   Pali — Single Go Binary                     │
│                                                              │
│  ┌─────────────────────┐  ┌──────────────┐  ┌────────────┐  │
│  │ REST API             │  │ MCP Server   │  │ Dashboard  │  │
│  │ /v1/memory           │  │ (stdio)      │  │ (HTMX)     │  │
│  │ /v1/memory/search    │  └──────┬───────┘  └─────┬──────┘  │
│  │ /v1/tenants          │         └────────┬────────┘         │
│  └──────────┬───────────┘                  │                  │
│             └──────────────────────────────┘                 │
│                            │                                 │
│                            ▼                                 │
│         ┌────────────────────────────────────────┐           │
│         │   Retrieval + Scoring Engine (WMR)      │           │
│         │   score = recency + relevance + importance│         │
│         └──────────────┬─────────────────────────┘          │
│                            │                                  │
│               ┌────────────▼─────────────┐                     │
│               │   VectorStore interface  │                     │
│               └──┬──────────┬───────────┘                     │
│                  │          │           │                     │
│          ┌───────▼──┐ ┌─────▼────┐ ┌───▼──────┐              │
│          │sqlite-vec│ │  Qdrant  │ │ pgvector │              │
│          │(default) │ │(opt-in)  │ │(opt-in)  │              │
│          └───────┬──┘ └──────────┘ └──────────┘              │
│                  │                                            │
│    ┌─────────────▼───────────────────────────┐                 │
│    │         SQLite — Single File            │                 │
│    │  memories, tenants, tags (metadata)     │                 │
│    │  vec_* tables (sqlite-vec, default)     │                 │
│    └─────────────────────────────────────────┘                 │
│                                                              │
│                   ┌──────────────────────┐                   │
│                   │ Embedding Engine      │                   │
│                   │ MiniLM-L6-v2 (ONNX)  │                   │
│                   └──────────────────────┘                   │
└──────────────────────────────────────────────────────────────┘
```

| Component | Technology | Purpose |
|---|---|---|
| **Language** | Go 1.21+ | Single binary, performance, concurrency |
| **HTTP Framework** | gin-gonic/gin | REST API routing and middleware |
| **Database** | SQLite (ncruces/go-sqlite3) | Metadata storage, default vector store |
| **Vector Search** | sqlite-vec / Qdrant / pgvector | Pluggable embedding storage and retrieval |
| **Embeddings** | ONNX Runtime + MiniLM-L6-v2 | Local semantic encoding (384-dim) |
| **Dashboard** | Go templates + HTMX | Zero-dependency web UI |
| **MCP Protocol** | MCP Go SDK | Tool integration for AI clients |
| **Importance Scoring** | TF-IDF (default) / Ollama (opt-in) | Memory ranking component |

### Design Principles

- **Dependency Inversion**: All infrastructure components implement domain interfaces
- **Repository Pattern**: Abstract data access behind interfaces for backend flexibility
- **Hexagonal Architecture**: Core business logic isolated from delivery mechanisms (HTTP/MCP)
- **Zero External Dependencies**: Default configuration runs without external services

---

## Advanced Configuration

### LLM-Based Importance Scoring

For improved memory importance ranking, enable Ollama-based scoring:

```yaml
importance_scorer: ollama  # default: heuristic

ollama:
  base_url: http://localhost:11434
  model: deepseek-r1:7b
  timeout_ms: 2000
```

**Performance impact:** Adds 100-500ms per store operation. Default TF-IDF scorer has zero overhead.

**When to use:** Complex memory content requiring nuanced importance evaluation.

### Authentication

Production deployments should enable authentication:

```yaml
auth:
  enabled: true
  mode: bearer
  tokens:
    - token: "sk-prod-abc123"
      tenant_ids: ["tenant_1", "tenant_2"]
    - token: "sk-prod-xyz789"
      tenant_ids: ["tenant_3"]
```

**Authentication modes:**
- **Bearer token**: HTTP header `Authorization: Bearer <token>`
- **MCP authentication**: Via environment variable `PALI_TOKEN`

### Tenant Isolation

All operations are scoped to a single tenant:
- API requests include `tenant_id` parameter
- Auth middleware validates token access before query execution
- Vector backends enforce isolation (collections/tables/partitions)
- Dashboard provides per-tenant view filtering

### Backend Switching

TODO: HOW TO HANDLE SETUP FOR PGVECTOR POOR QDRANT

Change vector backend without code changes:

```yaml
# SQLite (embedded, default)
vector_backend: sqlite

# Qdrant (scale to millions)
vector_backend: qdrant
qdrant:
  host: localhost
  port: 6334
  collection_prefix: pali_

# pgvector (PostgreSQL)
vector_backend: pgvector
pgvector:
  dsn: postgres://user:pass@localhost:5432/pali
  pool_size: 10
```

See [deployment guide](docs/deployment.md) for production architecture recommendations.

---

## Performance

Preliminary benchmarks (single-node, sqlite-vec backend):

TODO: RUN THE ACTUAL BENCHMARKS 

**Test environment:** M1 MacBook Pro, 16GB RAM, local ONNX Runtime

**Note:** Qdrant/pgvector backends show improved performance at scale (>100k memories). See [benchmarks](docs/benchmarks.md) for detailed results.

---

## Production Considerations

### Limitations

| Constraint | Impact | Mitigation |
|---|---|---|
| sqlite-vec uses KNN (no ANN) | Linear scan for vector search | Switch to Qdrant/pgvector for >100k memories |
| SQLite write concurrency | Serialized writes under load | Use Qdrant/pgvector for high-concurrency deployments |
| Static WMR weights | One weight set per instance | Per-tenant tuning roadmapped for v0.6 |
| ONNX Runtime dependency | Requires system library | Setup script automates installation |

### Scaling Guidelines

- **0-50k memories**: sqlite-vec sufficient for most workloads
- **50k-500k memories**: Consider Qdrant for better indexing
- **500k+ memories**: Qdrant or pgvector recommended
- **High concurrency**: Qdrant or pgvector with connection pooling

---

## Development

### Building from Source

```bash
git clone https://github.com/yourusername/pali.git
cd pali
go mod download
go build -o pali ./cmd/pali
```

### Running Tests

```bash
# Unit tests
go test ./...

# Integration tests
go test -tags=integration ./test/integration/...

# E2E tests
go test -tags=e2e ./test/e2e/...

# Benchmarks
go test -bench=. -benchmem ./...
```

### Contributing

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

**Areas of focus:**
- Additional vector backend implementations
- WMR scoring algorithm improvements
- Dashboard features (memory editing, conflict resolution UI)
- Performance optimizations
- Documentation and examples

---

## Roadmap

### Current Status: Beta (v0.1)

✅ **Completed**
- REST API with memory CRUD operations
- SQLite metadata storage + sqlite-vec default backend
- ONNX Runtime embeddings (MiniLM-L6-v2)
- WMR scoring with three-factor ranking
- MCP server with core tools
- Basic dashboard for memory inspection
- Bearer token authentication
- Multi-tenant isolation

🚧 **In Progress**
- Production deployment documentation
- Comprehensive benchmark suite
- Docker distribution

📋 **Planned**

**v0.2** — Enhanced tenant management
- Extended tenant statistics API
- Usage analytics and reporting
- Tenant quota enforcement

**v0.3** — Scale-out backends
- Qdrant integration
- pgvector integration
- Backend migration tooling

**v0.4** — Advanced scoring
- Ollama-based importance scoring
- Pluggable scoring pipeline
- Custom scoring functions

**v0.5** — Dashboard improvements
- In-place memory editing
- Conflict resolution UI
- Recall history visualization

**v0.6** — Memory lifecycle automation
- Per-tenant WMR weight tuning
- Auto-promotion episodic → semantic
- Scheduled consolidation jobs

**v1.0** — Production hardening
- Stable API guarantee
- Full platform support (Linux/macOS/Windows)
- Production deployment templates
- Comprehensive observability

---

## Support

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/yourusername/pali/issues)
- **Discussions**: [GitHub Discussions](https://github.com/yourusername/pali/discussions)
- **Security**: See [SECURITY.md](SECURITY.md) for reporting vulnerabilities

---

## License

MIT License - see [LICENSE](LICENSE) for details.

---

## Acknowledgments

Pali's memory architecture is informed by research from:
- Park et al. (2023) - *Generative Agents: Interactive Simulacra of Human Behavior*
- Zhong et al. (2024) - *MemoryBank: Enhancing Large Language Models with Long-Term Memory* (AAAI 2024)

Built with Go, SQLite, ONNX Runtime, and [these open source projects](docs/acknowledgments.md).