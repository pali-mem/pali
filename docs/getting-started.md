# Getting Started

This quick path gets Pali running locally and verifies a full tenant memory flow.

If you already installed the release binary from the README, use:

```bash
pali init
pali serve
```

If you prefer a source checkout, use:

```bash
make setup
make run
```

Default address: `http://127.0.0.1:8080`

Health check:

```bash
curl http://127.0.0.1:8080/health
```

## 1) Create a tenant

```bash
curl -X POST http://127.0.0.1:8080/v1/tenants \
  -H "Content-Type: application/json" \
  -d '{"id":"tenant_a","name":"Tenant A"}'
```

## 2) Store memory

```bash
curl -X POST http://127.0.0.1:8080/v1/memory \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"tenant_a","content":"User likes jasmine tea."}'
```

## 3) Search memory

```bash
curl -X POST http://127.0.0.1:8080/v1/memory/search \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"tenant_a","query":"tea"}'
```

## Next steps

- [Configuration](configuration.md) for canonical defaults and validation behavior
- [Deployment](deployment.md) for production packaging and runtime setup
- [Operations](operations.md) for runbook and rollback checklist
- [Multi-Tenancy](multitenancy.md) if you want JWT-scoped tenant isolation
