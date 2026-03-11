# Contributing

Pali is close to first public release. Until the team formalizes a heavier process, assume a simple rule:

- core maintainers may commit directly to `main`
- outside contributors should still open a PR unless explicitly asked otherwise

The important thing is not ceremony. It is keeping the repo shippable.

## Quality Bar

Every change should leave the repo in a better state than it found it.

Minimum expectations:

- code is readable and intentionally structured
- behavior is covered by the smallest meaningful test scope
- docs and examples stay aligned with runtime behavior
- config changes update `internal/config/defaults.go`, `pali.yaml.example`, and `docs/configuration.md` together
- release-facing changes do not introduce junk artifacts, stale paths, or benchmark ambiguity

## Before Merging or Pushing to `main`

Run the relevant checks for the change.

For release-facing, config, docs, CI, setup, or benchmark work, run:

```bash
bash ./scripts/release_gate.sh
```

For narrower changes, run the smallest relevant test target plus any impacted integration path.

Examples:

- `go test ./internal/... ./pkg/... ./cmd/...`
- `go test -tags integration ./test/integration/...`
- `go test -tags e2e ./test/e2e/...`

## Benchmark Changes

If you touch retrieval behavior, defaults, or release claims:

- use the checked-in assets under `testdata/benchmarks/`
- prefer the wrappers under `test/benchmarks/profiles/`
- keep benchmark claims tied to the exact recorded config
- do not present research-only runs as the public baseline

Each benchmark result should be reproducible from:

- fixture path and hash
- eval-set path and hash
- provider profile
- rendered runtime config

## Docs Discipline

If a command, path, config field, or default changes, update the docs in the same pass.

At minimum, check the affected subset of:

- `README.md`
- `docs/configuration.md`
- `docs/deployment.md`
- `cmd/setup/README.md`
- `BENCHMARKS.MD`
- `CHANGELOG.md`

## What To Avoid

- speculative refactors without a concrete payoff
- silent config drift
- committing generated binaries, sqlite/db files, benchmark temp output, or research artifacts
- weakening tests or docs to hide a real behavior mismatch

## Bug Reports and Fixes

When reporting or fixing a bug, include:

- the config or provider profile involved
- the exact command, API request, or workflow
- expected behavior
- actual behavior
- the test added or updated to keep it fixed
