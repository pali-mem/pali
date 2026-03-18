# Docs Map

Pali is open memory infrastructure for LLM and agent systems. This page is the fastest way to navigate docs by role.

## Choose your path

### User

- [Getting Started](getting-started.md)
- [Docker Deployment](deployment.md#docker)
- [Deployment](deployment.md)
- [Operations](operations.md)

### Developer

- [Configuration](configuration.md)
- [API](api.md)
- [MCP Integration](mcp.md)
- [Go Client SDK](client/README.md)
- [Python SDK (`pali-py`)](https://github.com/pali-mem/pali-py)
- [JavaScript SDK (`pali-js`)](https://github.com/pali-mem/pali-js)

### Future maintainer

- [Architecture](architecture.md)
- [Multi-Tenancy](multitenancy.md)
- [Benchmark Policy (GitHub)](https://github.com/pali-mem/pali/blob/main/BENCHMARKS.MD)

## Fast start

```bash
make setup
make run
curl http://127.0.0.1:8080/health
```

Then continue with [Getting Started](getting-started.md) for tenant creation plus first store/search calls.

Container-first quick path:

```bash
docker build -t pali:local .
docker run --rm -p 8080:8080 -v pali-data:/var/lib/pali pali:local
curl http://127.0.0.1:8080/health
```

## Recommended reading order

1. [Project overview (GitHub)](https://github.com/pali-mem/pali/blob/main/README.md)
2. [Getting Started](getting-started.md)
3. [Multi-Tenancy](multitenancy.md)
4. [Configuration](configuration.md)
5. [Deployment](deployment.md)
6. [Operations](operations.md)
7. [API](api.md)
8. [MCP Integration](mcp.md)
9. [Architecture](architecture.md)
10. [Benchmark Policy (GitHub)](https://github.com/pali-mem/pali/blob/main/BENCHMARKS.MD)

## Core docs

| Area | Docs |
|---|---|
| Setup and operations | [Getting Started](getting-started.md), [Deployment](deployment.md), [Operations](operations.md) |
| Integration | [API](api.md), [MCP Integration](mcp.md), [Go Client SDK](client/README.md), [Python SDK](https://github.com/pali-mem/pali-py), [JavaScript SDK](https://github.com/pali-mem/pali-js) |
| Runtime behavior | [Configuration](configuration.md), [Multi-Tenancy](multitenancy.md), [ONNX Runtime](onnx.md) |
| System design | [Architecture](architecture.md) |

## Maintainer references

- [Contributing (GitHub)](https://github.com/pali-mem/pali/blob/main/CONTRIBUTING.md)
- [Changelog (GitHub)](https://github.com/pali-mem/pali/blob/main/CHANGELOG.md)
- [Benchmark Policy (GitHub)](https://github.com/pali-mem/pali/blob/main/BENCHMARKS.MD)
- [Implementation change notes (GitHub)](https://github.com/pali-mem/pali/tree/main/docs/changes)

## Notes on scope

- `docs/internal/*` and dated `docs/changes/*` are kept in-repo for maintainers.
- The public docs site focuses on stable user/developer docs and links out to maintainer artifacts where needed.
