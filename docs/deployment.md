# Deployment

## Production Readiness

- Run only with explicit config artifacts (do not commit secrets into `pali.yaml`).
- Put Pali behind TLS-terminating reverse proxy (Nginx/Caddy/Envoy).
- Run with a process supervisor and restart policy.
- Persist the SQLite DB file and back it up on a schedule.
- Validate startup using `cmd/setup` in release checks.
- Add readiness checks against `/health` and monitor startup logs.

## Build

```bash

go build -o bin/pali ./cmd/pali
```

Optional install to PATH:

```bash
make install
pali -config /etc/pali/pali.yaml
```

User-local PATH install (no sudo):

```bash
make install PREFIX="$HOME/.local"
export PATH="$HOME/.local/bin:$PATH"
pali -config pali.yaml
```

## Configure

1. Bootstrap the config file you want to run:

```bash
go run ./cmd/setup -config /etc/pali/pali.yaml -skip-model-download
```

2. Edit `/etc/pali/pali.yaml` for your environment.
   - Full reference: `docs/configuration.md`
   - Multi-tenant/auth model: `docs/multitenancy.md`
   - Required secrets can come from env fallbacks:
     - `OPENROUTER_API_KEY`
     - `NEO4J_PASSWORD`
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
./bin/pali -config /etc/pali/pali.yaml
```

For local/dev:

```bash
./bin/pali -config pali.yaml
```

## Run MCP

```bash
./bin/pali mcp run -config /etc/pali/pali.yaml
```

For local/dev:

```bash
./bin/pali mcp run -config pali.yaml
```

## Deployment Patterns

### Docker

- Mount `/etc/pali` as a read-only config/data volume.
- Ensure DB path points to persistent storage, not ephemeral container storage.
- Enable restart policy and wire `/health` into container probes.

### systemd

- Set `ExecStart` with absolute binary/config paths.
- Set `Restart=always` and `RestartSec=2s`.
- Restrict file permissions on config and DB.
- Keep logs on the host journal or stdout capture.

Before starting the service, run:

```bash
go run ./cmd/setup -config /etc/pali/pali.yaml
```

to validate provider prerequisites and ensure the target config file exists.

For a full production runbook (health checks, rollback, backup/recovery, incident checklist), use [`operations.md`](operations.md) and treat it as the post-deploy/on-call reference.

Operator note:
- the dashboard is useful for inspecting tenants and memories, but it is not currently protected by the `/v1` JWT middleware
- if you expose Pali outside a trusted network, put dashboard access behind your reverse proxy or another auth layer
