# Multi-Tenancy

Pali is built to serve multiple tenants from one deployment, but the isolation model is intentionally simple:

- the REST API is tenant-scoped
- JWT auth, when enabled, is single-tenant per token
- MCP resolves a tenant for each tool call
- the dashboard is an operator surface, not a tenant-facing authenticated console

## REST API Isolation

When `auth.enabled: true`, all `/v1` routes require `Authorization: Bearer <jwt>`.

The JWT must include:

- `tenant_id`

Current API behavior:

- the token tenant is loaded into request context by auth middleware
- tenant-scoped handlers compare the JWT tenant with the tenant in the body, query, or path
- mismatches return `403`
- missing or invalid JWTs return `401`

This means one token acts as one tenant. Pali does not currently expose an admin token that can operate across many tenants through the normal API handlers.

## What A Tenant-Scoped Token Can Do

A token for `tenant_a` can:

- create `tenant_a`
- store memory in `tenant_a`
- ingest memory in `tenant_a`
- search memory in `tenant_a`
- list jobs for `tenant_a`
- delete memory in `tenant_a`
- read stats for `tenant_a`

The same token cannot operate on `tenant_b`.

## MCP Tenant Resolution

MCP uses the same underlying tenant and memory services, but tenant resolution is broader because MCP hosts differ in what metadata they send.

Resolution order:

1. explicit `tenant_id` in tool input
2. JWT tenant claim, when auth is enabled and the host forwards it
3. MCP session default tenant
4. `default_tenant_id` from config
5. otherwise the tool returns an error

This is useful for agent hosts, but it is different from the HTTP API's strict bearer-token model. If you need hard tenant-bound auth, test the REST API path directly.

## Dashboard Behavior

The dashboard is useful for operators, not end users.

Today:

- tenant and memory listings come from the SQLite-backed repository layer
- search and retrieval-backed views still pass through the core memory service
- configured vector and graph backends influence recall and ranking, but they are not the dashboard's source of truth for persisted memory rows
- dashboard routes are not currently protected by the same JWT middleware as `/v1`

That means dashboard listing still works even when you enable Qdrant or Neo4j. Those systems extend retrieval behavior; they do not replace the repository used to render persisted memories.

## Config Knobs That Matter

Relevant settings in [`configuration.md`](configuration.md) and [`pali.yaml.example` (GitHub)](https://github.com/pali-mem/pali/blob/main/pali.yaml.example):

- `auth.enabled`
- `auth.jwt_secret`
- `auth.issuer`
- `default_tenant_id`
- `vector_backend`
- `entity_fact_backend`
- `retrieval.multi_hop.*`

## Recommended Pre-Deploy Checks

Before calling a deployment multi-tenant ready:

1. enable `auth.enabled: true`
2. mint two JWTs with different `tenant_id` claims
3. create and write memory for tenant A
4. confirm tenant A token cannot read or write tenant B
5. test MCP with explicit `tenant_id` and with session/default tenant fallback
6. verify your reverse proxy or network layer protects the dashboard if you expose it outside a trusted environment

## Example Dev Flow

Mint a token:

```bash
go run ./cmd/jwt -tenant tenant_a -secret "change-me"
```

Create a tenant:

```bash
curl -X POST http://127.0.0.1:8080/v1/tenants \
  -H "Authorization: Bearer <jwt>" \
  -H "Content-Type: application/json" \
  -d '{"id":"tenant_a","name":"Tenant A"}'
```

Store a memory:

```bash
curl -X POST http://127.0.0.1:8080/v1/memory \
  -H "Authorization: Bearer <jwt>" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"tenant_a","content":"User likes jasmine tea."}'
```

Test mismatch rejection:

```bash
curl -X POST http://127.0.0.1:8080/v1/memory/search \
  -H "Authorization: Bearer <jwt>" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id":"tenant_b","query":"tea"}'
```

Expected result: `403`.
