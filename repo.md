pali/
├── cmd/
│   ├── pali/                    # Main server binary
│   │   └── main.go
│   └── setup/                   # Setup command for ONNX/models
│       └── main.go
│
├── internal/
│   ├── domain/                  # Core domain models & interfaces (no deps on other internal packages)
│   │   ├── memory.go           # Memory entity, MemoryTier enum
│   │   ├── tenant.go           # Tenant entity
│   │   ├── repository.go       # Repository interfaces
│   │   ├── vectorstore.go      # VectorStore interface
│   │   ├── scorer.go           # ImportanceScorer interface
│   │   ├── embedder.go         # Embedder interface
│   │   └── errors.go           # Domain-specific errors
│   │
│   ├── core/                    # Business logic / use cases
│   │   ├── memory/             # Memory use cases
│   │   │   ├── store.go        # Store memory use case
│   │   │   ├── search.go       # Search/retrieve use case
│   │   │   ├── delete.go       # Delete memory use case
│   │   │   └── service.go      # Memory service orchestration
│   │   ├── tenant/             # Tenant use cases
│   │   │   ├── create.go
│   │   │   ├── stats.go
│   │   │   ├── isolation.go    # Tenant isolation business rules
│   │   │   └── service.go
│   │   ├── scoring/            # WMR scoring engine
│   │   │   ├── wmr.go          # Main WMR formula
│   │   │   ├── recency.go      # Recency calculation (Ebbinghaus)
│   │   │   ├── relevance.go    # Cosine similarity wrapper
│   │   │   └── normalizer.go   # Min-max normalization
│   │   └── retrieval/          # Two-phase retrieval logic
│   │       ├── retriever.go
│   │       └── ranker.go
│   │
│   ├── repository/              # Repository implementations
│   │   └── sqlite/
│   │       ├── memory.go       # SQLite memory repository
│   │       ├── memory_test.go  # Memory repository tests
│   │       ├── tenant.go       # SQLite tenant repository
│   │       ├── tenant_test.go  # Tenant repository tests
│   │       ├── migrations.go   # Schema setup (embedded Go)
│   │       └── queries.go      # SQL queries
│   │
│   ├── vectorstore/             # Vector store implementations
│   │   ├── sqlitevec/          # Default embedded
│   │   │   ├── store.go
│   │   │   ├── store_test.go
│   │   │   └── queries.go
│   │   ├── qdrant/             # Opt-in
│   │   │   ├── store.go
│   │   │   ├── store_test.go
│   │   │   └── client.go
│   │   ├── pgvector/           # Opt-in
│   │   │   ├── store.go
│   │   │   ├── store_test.go
│   │   │   └── queries.go
│   │   └── mock/               # Mock for testing
│   │       └── store.go
│   │
│   ├── embeddings/              # Embedding engine
│   │   ├── onnx/
│   │   │   ├── embedder.go     # ONNX Runtime wrapper
│   │   │   ├── embedder_test.go
│   │   │   ├── tokenizer.go    # MiniLM tokenizer
│   │   │   └── loader.go       # Model loading
│   │   └── mock/
│   │       └── embedder.go
│   │
│   ├── scorer/                  # Importance scorer implementations
│   │   ├── heuristic/
│   │   │   ├── scorer.go       # TF-IDF + heuristics
│   │   │   └── scorer_test.go
│   │   ├── ollama/
│   │   │   ├── scorer.go       # Ollama via cohesion-org/deepseek-go
│   │   │   ├── scorer_test.go
│   │   │   └── client.go
│   │   └── mock/
│   │       └── scorer.go
│   │
│   ├── auth/                    # Authentication & authorization
│   │   ├── auth.go             # Auth interfaces
│   │   ├── bearer.go           # Bearer token auth
│   │   ├── middleware.go       # HTTP middleware
│   │   └── auth_test.go
│   │
│   ├── api/                     # REST API (HTTP delivery layer)
│   │   ├── router.go           # Gin router setup
│   │   ├── middleware/         # HTTP middleware
│   │   │   ├── cors.go
│   │   │   ├── logging.go
│   │   │   └── recovery.go
│   │   ├── handlers/
│   │   │   ├── memory.go       # /v1/memory endpoints
│   │   │   ├── tenant.go       # /v1/tenants endpoints
│   │   │   ├── health.go       # /health
│   │   │   └── handlers_test.go
│   │   └── dto/                # Request/response DTOs
│   │       ├── memory.go
│   │       └── tenant.go
│   │
│   ├── mcp/                     # MCP server (stdio delivery)
│   │   ├── server.go           # MCP server setup
│   │   ├── tools/              # MCP tool implementations
│   │   │   ├── memory_store.go
│   │   │   ├── memory_search.go
│   │   │   ├── memory_delete.go
│   │   │   └── tenant_stats.go
│   │   └── server_test.go
│   │
│   ├── dashboard/               # Dashboard (HTMX UI)
│   │   ├── handlers.go         # Dashboard HTTP handlers
│   │   ├── templates/          # Go templates
│   │   │   ├── layout.html
│   │   │   ├── memories.html
│   │   │   ├── tenants.html
│   │   │   └── stats.html
│   │   └── handlers_test.go
│   │
│   └── config/                  # Configuration
│       ├── config.go           # Config struct & loading
│       ├── defaults.go         # Default values
│       ├── validation.go       # Config validation
│       └── config_test.go
│
├── pkg/                         # Public/reusable packages (if exposing SDK)
│   └── client/                 # Optional: Go client for Pali API (NOT v0.1 scope)
│       ├── client.go
│       └── client_test.go
│
├── test/                        # Integration & E2E tests
│   ├── integration/
│   │   ├── memory_flow_test.go
│   │   ├── tenant_test.go
│   │   └── wmr_test.go
│   ├── e2e/
│   │   ├── api_test.go
│   │   └── mcp_test.go
│   ├── fixtures/               # Test data
│   │   ├── memories.json
│   │   └── tenants.json
│   └── testutil/               # Test helpers
│       ├── db.go              # In-memory SQLite setup
│       ├── mocks.go
│       └── assertions.go
│
├── scripts/                     # Operational scripts
│   ├── setup.sh               # ONNX setup (Linux/macOS)
│   ├── setup.ps1              # ONNX setup (Windows)
│   └── benchmark.sh           # Benchmark harness runner
│
├── models/                      # ONNX models & artifacts
│   ├── all-MiniLM-L6-v2/
│   │   ├── model.onnx
│   │   ├── tokenizer.json
│   │   └── checksums.txt
│   └── .gitkeep
│
├── web/                         # Static assets for dashboard
│   ├── static/
│   │   ├── css/
│   │   │   └── dashboard.css
│   │   └── js/
│   │       └── htmx.min.js
│   └── .gitkeep
│
├── docs/                        # Additional documentation
│   ├── architecture.md
│   ├── api.md                 # OpenAPI spec or detailed API docs
│   ├── mcp.md                 # MCP integration guide
│   └── deployment.md
│
├── .gitignore
├── go.mod
├── go.sum
├── Makefile                     # Build, test, lint targets
├── pali.yaml.example           # Example config
├── LICENSE
└── README.md