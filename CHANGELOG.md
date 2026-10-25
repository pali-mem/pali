# Changelog

## v0.2.1 - 2026-10-25

Full notes and benchmark deltas: [`changelog/v0.2.1.md`](changelog/v0.2.1.md).

### Fixed

- ranking: routed (temporal / person / multi-hop) queries no longer saturate top scores to `1.0`; final sort is deterministic
- retrieval: lexical candidates carry real FTS5 BM25 scores (SQLite) instead of rank position
- retrieval: a bare `and` between noun phrases no longer classifies a query as multi-hop
- parser: dedupe on normalized content + kind; month/time tags no longer match ordinary words; single-sentence utterances are not stored twice; pronouns are not graph entities; long low-signal text is filtered
- heuristic importance scorer: replaced a TF-IDF term that scored `"ok"` at 1.0
- repository: reading a memory no longer rewrites `updated_at`

### Removed

- no-op config keys `retrieval.temporal_resolver_enabled`, `retrieval.open_domain_alternative_resolver_enabled`, `parser.dedupe_threshold`, `parser.update_threshold` (old YAML still loads)
- dead retrieval package (no importers) and unused scoring helpers

### Changed

- `make lint` pins golangci-lint `v2.11.4` and installs it into `bin/tools`; CI uses the same script

## v0.2.0 - 2026-03-22

### Added

- config-aware `cmd/setup -config`
- explicit tagged CI jobs for integration and e2e suites
- docs freshness, docs example verification, dead-code sweep, and release gate scripts
- checked-in canonical benchmark fixture and eval set under `testdata/benchmarks/`
- checked-in benchmark profile wrappers under `test/benchmarks/profiles/`
- community repo files for contributing, conduct, and security

### Changed

- benchmark scripts now default to the canonical checked-in fixture and eval set
- benchmark result directories now include the source provider profile and rendered runtime config
- release docs, deployment docs, configuration docs, and benchmark docs were synced to current runtime behavior
- `pgvector` moved from scaffold-only to a supported vector backend with config, runtime wiring, and benchmark-script support
- added a first-party Docker Compose overlay for PostgreSQL + `pgvector`

### Notes

- latest retained March 9, 2026 LOCOMO raw runs remain documented as research-only context, not the release baseline
- `pgvector` requires a reachable PostgreSQL instance with the `vector` extension available
