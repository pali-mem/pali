# Operations Checklist

This is a quick-read ops page. For full detail, use [Operations](operations.md).

## Pre-deploy

1. Enable auth for non-dev environments.
2. Validate config: `go run ./cmd/setup -config pali.yaml`.
3. Confirm persistent storage path for SQLite DB files.
4. Verify health endpoint and startup logs in staging.

## Deploy

1. Start API mode with your production config.
2. Run smoke tests on `/health`, tenant create, and memory search.
3. Confirm dashboard exposure is restricted behind your network/proxy policy.

## Rollback-ready

1. Keep previous binary/image ready.
2. Keep recent DB backup/snapshot ready.
3. Document exact rollback command in your runbook.

## Post-deploy checks

1. Request latency and error rates are within expected bounds.
2. Tenant isolation checks pass across at least two tenants.
3. Retrieval quality spot checks are stable for known queries.

## Go deeper

- Full runbook: [Operations](operations.md)
- Runtime setup: [Deployment](deployment.md)
- Isolation model: [Multi-Tenancy](multitenancy.md)
