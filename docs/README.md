# Pali Docs

## User-facing

| Doc | Description |
|---|---|
| [api.md](api.md) | REST API reference — endpoints, request/response schemas, error codes |
| [configuration.md](configuration.md) | Full config reference (`pali.yaml`) — all fields, defaults, env fallbacks |
| [deployment.md](deployment.md) | Build, configure, and run Pali |
| [operations.md](operations.md) | Production operations checklist, backups, rollback, and incident response |
| [mcp.md](mcp.md) | MCP server — tools, tenant resolution, testing |
| [onnx.md](onnx.md) | ONNX Runtime setup for local embedding inference |
| [client/README.md](client/README.md) | Go client SDK — quickstart, methods, error handling |

## Architecture

| Doc | Description |
|---|---|
| [architecture.md](architecture.md) | Embedding providers, retrieval pipeline, hybrid ranking |

## Changes

Dated logs of behavior/performance-impacting changes: [changes/](changes/)

## Internal

Developer and contributor references — implementation details, planning, research.

| Doc | Description |
|---|---|
| [internal/sdk-architecture.md](internal/sdk-architecture.md) | SDK design rules and contracts for all client packages |
| [internal/sqlite.md](internal/sqlite.md) | SQLite layer — schema, connection settings, CRUD coverage |
| [internal/product-plan.md](internal/product-plan.md) | Product improvement priorities and gate ladder |
| [internal/retrieval-improvements.md](internal/retrieval-improvements.md) | Retrieval scoring analysis and improvement directions |
