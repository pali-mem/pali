# Pre-Release Checklist

This file tracks what still matters for `v0.1`, not historical plans that are already done or explicitly out of scope.

## Already in Place

- [x] REST API, dashboard, MCP server, and multi-tenant memory flows
- [x] Go client SDK in `pkg/client`
- [x] Async post-processing pipeline
- [x] Config-driven backend/provider wiring
- [x] Qdrant vector backend
- [x] Neo4j entity-fact backend
- [x] CI workflow, issue templates, dependabot, and label sync
- [x] Setup/bootstrap command with config-aware validation

## Must Finish Before Tagging `v0.1.0`

- [x] Keep `README.md`, `TODO.md`, `instructions.md`, and `docs/*` aligned with runtime behavior and defaults
- [x] Keep `docs/configuration.md`, `pali.yaml.example`, and `internal/config/defaults.go` in sync, including `retrieval.multi_hop`
- [x] Keep `cmd/setup` docs aligned with `-config` support and current provider checks
- [x] Use the checked-in benchmark assets under `testdata/benchmarks/` as the canonical release eval set
- [x] Record benchmark runs with the exact config used (`config.profile.yaml` + `config.rendered.yaml`)
- [x] Remove or ignore disposable repo artifacts before public tagging (`*.exe`, sqlite/db files, research caches, benchmark temp output)
- [x] Add and maintain community files: `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `CHANGELOG.md`
- [x] Run explicit tagged CI jobs for `integration` and `e2e`
- [x] Add docs freshness checks for broken links and missing referenced files
- [x] Add a release gate script that executes the documented setup/run examples
- [x] Run a dead-code and unused-file sweep before tagging

## Not Blocking `v0.1`

- [ ] Python SDK repo and PyPI publication
- [ ] pgvector adapter
- [ ] Docker and one-command cloud deployment flows
- [ ] richer tenant stats and dashboard v2 work

## Release Command

```bash
scripts/release_gate.sh
```
