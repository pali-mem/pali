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

## Configure

1. Copy the canonical template:

```bash
cp pali.yaml.example pali.yaml
```

2. Edit `pali.yaml` for your environment.
   - Full reference: `docs/configuration.md`
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

When deploying, run:

```bash
go run ./cmd/setup -config /etc/pali/pali.yaml
```

before starting service to validate prerequisites and provider readiness.

For a full production runbook (health checks, rollback, backup/recovery, incident checklist), use [`operations.md`](operations.md) and treat it as the post-deploy/on-call reference.
