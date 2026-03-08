# Async Postprocess Pipeline (2026-03-08)

## What Changed

- Added async ingest endpoints:
  - `POST /v1/memory/ingest`
  - `POST /v1/memory/ingest/batch`
- Added postprocess job observability endpoints:
  - `GET /v1/memory/jobs/:id`
  - `GET /v1/memory/jobs`
- Added `postprocess` runtime config block (`enabled`, polling, lease, retries, worker count).
- Added repository-backed queue table `memory_postprocess_jobs` for:
  - `parser_extract`
  - `vector_upsert`
- Added transactional async ingest path that stores memories and enqueues postprocess jobs in one SQLite transaction.
- Added in-process worker runtime for API/MCP processes with:
  - lease-based claiming
  - success/failure state transitions
  - exponential retry with dead-lettering
- Added parser job handling that stores derived canonical memories/entity facts and enqueues vector jobs for newly created derived rows.
- Kept existing sync endpoints unchanged (`POST /v1/memory`, `POST /v1/memory/batch` still return `201`).

## Why

- Reduce upload-path latency for opt-in async ingest.
- Provide mem0-style post-upload automation while keeping compatibility for existing clients.
- Improve reliability/operability with explicit queue state and retry lifecycle.

## Validation

- Added/updated tests for:
  - async ingest API + job endpoints
  - SQLite postprocess queue lifecycle (enqueue/claim/failure/success)
  - worker execution for vector and parser jobs
- Full test suite passed:
  - `go test ./...`
