# Pali Docs

## Read First

Use this order before you deploy or integrate Pali:

| Order | Doc | Why it matters |
|---|---|---|
| 1 | [../README.md](../README.md) | Product overview, runtime modes, and quickstart |
| 2 | [multitenancy.md](multitenancy.md) | Tenant isolation, JWT behavior, MCP tenant resolution, dashboard caveats |
| 3 | [configuration.md](configuration.md) | Canonical config defaults and validation rules |
| 4 | [deployment.md](deployment.md) | Build, configure, run, and package Pali |
| 5 | [operations.md](operations.md) | Production checklist and operator runbook |
| 6 | [api.md](api.md) | REST request and response contract |
| 7 | [mcp.md](mcp.md) | MCP runtime, tools, and tenant-aware behavior |
| 8 | [architecture.md](architecture.md) | Retrieval/storage model and extension points |
| 9 | [../BENCHMARKS.MD](../BENCHMARKS.MD) | Benchmark policy, fixtures, and latest retained results |

## User-Facing

| Doc | Description |
|---|---|
| [api.md](api.md) | REST API reference |
| [multitenancy.md](multitenancy.md) | Tenant isolation, JWT auth model, MCP tenant resolution, dashboard scope |
| [configuration.md](configuration.md) | Canonical `pali.yaml` reference |
| [deployment.md](deployment.md) | Build, configure, validate, and run Pali |
| [operations.md](operations.md) | Production checklist and rollback notes |
| [mcp.md](mcp.md) | MCP server commands, tools, and testing |
| [onnx.md](onnx.md) | ONNX Runtime setup notes |
| [client/README.md](client/README.md) | Go client SDK usage |

## Architecture

| Doc | Description |
|---|---|
| [architecture.md](architecture.md) | Retrieval, storage, dashboard, and extension architecture |

## Benchmarks and Changes

| Doc | Description |
|---|---|
| [../BENCHMARKS.MD](../BENCHMARKS.MD) | Canonical benchmark policy and latest retained runs |
| [changes/README.md](changes/README.md) | Dated implementation and performance notes |

## Internal

Only currently-present internal references are listed here.

| Doc | Description |
|---|---|
| [internal/sdk-architecture.md](internal/sdk-architecture.md) | SDK design notes |
| [internal/sqlite.md](internal/sqlite.md) | SQLite repository notes |
| [internal/multihop-graph-upgrade-plan.md](internal/multihop-graph-upgrade-plan.md) | Multi-hop and graph work plan |
