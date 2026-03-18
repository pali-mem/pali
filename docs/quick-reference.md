# Quick Reference

Use this page when you need the fastest path to common tasks.

## Run locally

```bash
make setup
make run
curl http://127.0.0.1:8080/health
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
