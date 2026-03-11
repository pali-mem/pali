# Operations and Production Checklist

This document captures the minimum runbook for running Pali in production-like environments.

## Pre-deploy checklist

- Keep configuration and data paths outside the repository and mounted from deployment secrets/config maps.
- Keep `jwt_secret`, provider API keys, and database credentials outside source control.
- Validate startup before rollout:
  - `go run ./cmd/setup -config /etc/pali/pali.yaml`
  - `scripts/verify_docs_examples.sh` for repo-level command drift
- For non-dev environments:
  - `auth.enabled: true`
  - long random `auth.jwt_secret` value
  - strong reverse-proxy TLS termination
- Ensure data persistence:
  - `database.sqlite_dsn` points to persistent storage, not ephemeral temp directories.
  - directory permissions are restricted to the service user.
- Confirm tenant/graph prerequisites for current profile:
  - Neo4j password provided when `entity_fact_backend: neo4j`
  - embeddings provider readiness checks pass

## Deployment and rollback

### API mode

```bash
./bin/pali -config /etc/pali/pali.yaml
```

### MCP mode

```bash
./bin/pali mcp run -config /etc/pali/pali.yaml
```

- Keep run-time supervisors enabled (systemd/Kubernetes/docker restart policy) and explicit health check loops.
- Maintain a rollback image or binary snapshot for immediate restore.
- Roll back by swapping config/binary and restarting to the last known-good version.

## Health and readiness verification

- Poll:
  - `curl -sf http://127.0.0.1:8080/health || exit 1`
- Validate tenant and memory flow:
  - `POST /v1/tenants`
  - `POST /v1/memory/search`
- Review startup logs for:
  - repository initialization
  - provider/adapter selection
  - startup counts and vectorstore/entity-fact backend initialization

## Backups and recovery

- Schedule regular snapshots for:
  - SQLite DB file
  - vector state (qdrant, if used)
  - Neo4j data (if enabled for entity facts)
- Test restore at least once per change window:
  1. Restore snapshot to a staging path.
  2. Start Pali with the restored config.
  3. Run the health and tenant/memory checks.

## Operational limits and known gaps

- Built-in rate limiting is not part of the core service; enforce it at gateway/proxy.
- There is no dedicated metrics endpoint in this version. Use proxy/app logs plus external monitoring of process and dependency health.
- SQLite single-node persistence remains simpler than highly replicated topologies; scale vector or graph stores for heavier multi-node operational needs.

## Incident first-response checklist

1. Check process and restart status in supervisor.
2. Validate network and TLS path from gateway to Pali.
3. Run `/health` and tenant auth checks.
4. Review startup and request logs for recent migration/provider failures.
5. Validate disk and backing store availability (SQLite/Qdrant/Neo4j).
6. If required, roll back to the last known-good deployment and preserve evidence for post-mortem.
