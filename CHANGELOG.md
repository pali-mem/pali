# Changelog

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
