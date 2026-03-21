# Quick Reference

Use this page when you need the fastest path to common tasks.

## Run locally

```bash
pali init
pali serve
curl http://127.0.0.1:8080/health
```

From a source checkout:

```bash
make setup
make run
curl http://127.0.0.1:8080/health
```

## Run in Docker

```bash
docker build -t pali:local .
docker run --rm -p 8080:8080 -v pali-data:/var/lib/pali pali:local
curl http://127.0.0.1:8080/health
```

For one-command stacks with optional services, use:

```bash
docker compose -f deploy/docker/compose.yaml up --build
docker compose -f deploy/docker/compose.yaml -f deploy/docker/compose.qdrant.yaml up --build
```

## Most-used docs

- [Configuration](configuration.md)
- [Deployment](deployment.md)
- [Operations](operations.md)
- [API](api.md)
- [MCP Integration](mcp.md)

## Common checks

```bash
make test
make docs-build
```

## Tenant auth basics

- [Multi-Tenancy](multitenancy.md)
- JWT tenant mismatch returns `403` on `/v1` routes.
- Use explicit `tenant_id` for deterministic MCP behavior.

## Useful repo links

- [README (GitHub)](https://github.com/pali-mem/pali/blob/main/README.md)
- [Benchmark Policy (GitHub)](https://github.com/pali-mem/pali/blob/main/BENCHMARKS.MD)
- [Contributing (GitHub)](https://github.com/pali-mem/pali/blob/main/CONTRIBUTING.md)
- [Python SDK (`pali-py`)](https://github.com/pali-mem/pali-py)
- [JavaScript SDK (`pali-js`)](https://github.com/pali-mem/pali-js)
