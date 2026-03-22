# Deployment

## Production Readiness

- Run only with explicit config artifacts (do not commit secrets into `pali.yaml`).
- Put Pali behind TLS-terminating reverse proxy (Nginx/Caddy/Envoy).
- Run with a process supervisor and restart policy.
- Persist the SQLite DB file and back it up on a schedule.
- Validate startup using `pali init` in release checks.
- Add readiness checks against `/health` and monitor startup logs.

## Install

Release binary install is the fastest native path:

macOS/Linux:

```bash
curl -fsSL https://raw.githubusercontent.com/pali-mem/pali/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/pali-mem/pali/main/scripts/install.ps1 | iex
```

Then initialize and run:

```bash
pali init
pali serve
```

## Build

```bash

go build -o bin/pali ./cmd/pali
```

Optional install to PATH:

```bash
make install
pali serve -config /etc/pali/pali.yaml
```

User-local PATH install (no sudo):

```bash
make install PREFIX="$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
pali serve -config pali.yaml
```

## Configure

1. Bootstrap the config file you want to run:

```bash
pali init -config /etc/pali/pali.yaml -skip-model-download
```

2. Edit `/etc/pali/pali.yaml` for your environment.
   - Full reference: `docs/configuration.md`
   - Multi-tenant/auth model: `docs/multitenancy.md`
   - Required secrets can come from env fallbacks:
     - `OPENROUTER_API_KEY`
     - `NEO4J_PASSWORD`
   - Containerized deployments can also use explicit `PALI_*` environment overrides, for example:
     - `PALI_SERVER_HOST`
     - `PALI_DATABASE_SQLITE_DSN`
     - `PALI_VECTOR_BACKEND`
     - `PALI_QDRANT_BASE_URL`
     - `PALI_NEO4J_PASSWORD`
     - `PALI_AUTH_JWT_SECRET`
   - All other sensitive values should come from your deployment secret management strategy (config templating, Vault, SSM, etc.).

Recommended production layout:

```text
/etc/pali/
  +- pali.yaml
  +- pali.db
  +- bin/pali
```

Health checks:

```bash
curl -sf http://127.0.0.1:8080/health || exit 1
```

## Run API

```bash
./bin/pali serve -config /etc/pali/pali.yaml
```

For local/dev:

```bash
./bin/pali serve -config pali.yaml
```

## Run MCP

```bash
./bin/pali mcp serve -config /etc/pali/pali.yaml
```

For local/dev:

```bash
./bin/pali mcp serve -config pali.yaml
```

## Deployment Patterns

### Docker

Base image build:

```bash
docker build -t pali:local .
```

Run the base zero-dependency profile:

```bash
docker run --name pali \
  -p 8080:8080 \
  -v pali-data:/var/lib/pali \
  pali:local
```

The image default command is:

```bash
/app/pali -config /etc/pali/pali.yaml
```

The baked container config:

- binds to `0.0.0.0:8080`
- persists SQLite at `/var/lib/pali/pali.db`
- points optional services at `qdrant`, `neo4j`, and `ollama`

Override settings with:

- a mounted config file at `/etc/pali/pali.yaml`
- explicit `PALI_*` environment variables

Compose stacks:

```bash
docker compose -f deploy/docker/compose.yaml up --build
docker compose -f deploy/docker/compose.yaml -f deploy/docker/compose.qdrant.yaml up --build
docker compose -f deploy/docker/compose.yaml -f deploy/docker/compose.neo4j.yaml up --build
docker compose -f deploy/docker/compose.yaml -f deploy/docker/compose.ollama.yaml up --build
```

Notes:

- `compose.qdrant.yaml` switches `vector_backend` to `qdrant`
- `compose.neo4j.yaml` switches `entity_fact_backend` to `neo4j`
- `compose.ollama.yaml` starts Ollama and points the Ollama URLs at that service, but you still need to pull the model before enabling Ollama-backed embedding/parser/scorer
- Docker health checks are wired to `/health`, Qdrant `/healthz`, Neo4j `cypher-shell`, and `ollama list`

For Compose secrets and port overrides, start from `deploy/docker/.env.example`.

### systemd

- Set `ExecStart` with absolute binary/config paths.
- Set `Restart=always` and `RestartSec=2s`.
- Restrict file permissions on config and DB.
- Keep logs on the host journal or stdout capture.

Before starting the service, run:

```bash
pali init -config /etc/pali/pali.yaml
```

to validate provider prerequisites and ensure the target config file exists.

For a full production runbook (health checks, rollback, backup/recovery, incident checklist), use [`operations.md`](operations.md) and treat it as the post-deploy/on-call reference.

Operator note:
- the dashboard is useful for inspecting tenants and memories, but it is not currently protected by the `/v1` JWT middleware
- if you expose Pali outside a trusted network, put dashboard access behind your reverse proxy or another auth layer
