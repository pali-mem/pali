# Pre-Release Checklist

> Things that must be done before calling Pali ready for external users.
> Ordered by impact. Check off as completed.

---

## 1. Developer Experience (Highest Priority)

### Python SDK
- [ ] Create a new project inside pali-mem org, and create a pip-installable package (`pali-client`) in repo pali-python.
- [ ] Implement `PaliClient` — thin HTTP wrapper over the existing REST API (store, search, delete, tenants)
- [ ] Implement `PaliMiddleware` — wraps any OpenAI-compatible LLM client: search before call → inject context → call LLM → store new facts after
- [ ] Publish to PyPI as `pali-client`
- [ ] Add a `python/README.md` with a 5-line quickstart example

### Go Middleware
- [ ] Create `pkg/middleware` — same pattern as Python: wraps any LLM call with search-inject-store
- [ ] Works against the HTTP API (uses `pkg/client` internally)
- [ ] Add example in `pkg/middleware/example_test.go`

### Zero-dependency Quickstart
- [ ] Make ONNX the documented "quickstart" embedder (bundled model, no external service)
- [ ] Update README quickstart to use `embedding.provider: onnx` not Ollama — Ollama is "advanced"
- [ ] Label `embedding.provider: lexical` clearly as "CI/testing only, not for real use"
- [ ] Verify `make setup` + `make run` works on a fresh machine with no Ollama installed

---

## 2. Deployment (Blocking for Self-Hosters)

### Docker
- [ ] Write `docker/Dockerfile` — multi-stage build, minimal final image
- [ ] Write `docker/docker-compose.yml` — pali + qdrant, volume-mounted config
- [ ] Write `docker/docker-compose.ollama.yml` — optional Ollama sidecar override
- [ ] Write `docker/pali.yaml.docker` — pre-filled config for docker networking (qdrant at `http://qdrant:6333`)
- [ ] Test `docker compose up` cold start end-to-end

### One-Command Cloud Deploy
- [ ] Write `scripts/deploy-aws.sh` — builds image, pushes to ECR, updates ECS task definition
- [ ] Document environment variables that override `pali.yaml` at runtime (for 12-factor deploys)
- [ ] Add deploy section to `docs/deployment.md`

---

## 3. Production Readiness

### Pgvector
- [ ] Implement `internal/vectorstore/pgvector` adapter
- [ ] Wire it via `vector_backend: pgvector` in config
- [ ] Add integration tests
- [ ] Document in `docs/architecture.md`

### Operational Concerns (currently undocumented)
- [ ] Document memory limits — what happens at 100k+ memories per tenant with SQLite
- [ ] Document backup/restore for SQLite (`VACUUM INTO`, WAL copy)
- [ ] Add a `GET /metrics` endpoint (Prometheus-compatible) — at minimum: memory count, search latency p50/p99, store throughput
- [ ] Add structured logging option (JSON log lines) for production deployments
- [ ] Document recommended hardware specs for each embedding provider

---

## 4. Retrieval Quality (Technical Moat)

### Entity Store (P2 from todo_benchmarks.md)
- [ ] Design `entity_facts` table schema (entity, relation, value, memory_id)
- [ ] Implement entity extraction at store time (heuristic or Ollama)
- [ ] Implement aggregation query path: detect multi-hop intent → SELECT from entity table
- [ ] Benchmark against LOCOMO multi-hop: target F1 improvement from ~4% to 25–35%

### Embedding Bake-off (M4)
- [ ] Run `nomic-embed-text` vs `mxbai-embed-large` benchmark on LOCOMO dataset
- [ ] Pick a better default if evidence supports it
- [ ] Document results in `docs/changes/`

---

## 5. Documentation

- [ ] Expand `docs/deployment.md` — Docker, AWS, env var overrides, resource sizing
- [ ] Add `docs/quickstart.md` — zero to working in under 5 minutes
- [ ] Add `docs/sdk.md` — Go and Python SDK usage with real examples
- [ ] Add `docs/multitenancy.md` — tenant model, isolation guarantees, JWT setup
- [ ] Update README to reflect all three integration paths (REST, MCP, SDK)
- [ ] Add a `CONTRIBUTING.md` for when external contributors arrive

---

## 6. Repo Hygiene (Before Going Public)

- [ ] Decide on org name (e.g. `pali-mem`) and migrate from `pali-mem/pali` — update all `github.com/pali-mem/pali` import paths
- [ ] Add `LICENSE` check — confirm license is appropriate for an open infra project
- [ ] Add `CHANGELOG.md` — v0.1 entry with what's in it
- [ ] Tag `v0.1.0` on GitHub after CI is green

---

## 7. GitHub Automation

### One-time GitHub CLI Setup
```bash
# Install gh CLI (Windows)
winget install GitHub.cli

# Authenticate
gh auth login

# (When ready) Create org and transfer repo
gh org create pali-mem
gh repo transfer pali-mem/pali pali-mem

# Set repo description, topics, visibility
gh repo edit --description "Persistent memory layer for LLM applications" \
  --add-topic memory,llm,ai,rag,mcp,golang \
  --visibility public
```

### Branch Protection (run once after repo is public)
```bash
# Protect main: require PR + passing CI before merge
gh api repos/{owner}/pali/branches/main/protection \
  --method PUT \
  --field required_status_checks='{"strict":true,"contexts":["ci / test","ci / lint"]}' \
  --field enforce_admins=false \
  --field required_pull_request_reviews='{"required_approving_review_count":1}' \
  --field restrictions=null
```

### GitHub Actions — CI Pipeline
- [ ] Create `.github/workflows/ci.yml`:
  - Trigger: push to `main`, all PRs
  - Jobs:
    - `lint` — `golangci-lint run ./...`
    - `test` — `go test ./...` (unit only, fast)
    - `test-integration` — `go test ./test/integration/...` with SQLite
    - `build` — `go build ./cmd/pali`
  - Matrix: Go 1.24 on ubuntu-latest, windows-latest
  - Cache: `actions/cache` on `~/.cache/go`

```yaml
# .github/workflows/ci.yml skeleton
name: ci
on:
  push:
    branches: [main]
  pull_request:
jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - uses: golangci/golangci-lint-action@v6
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - run: go test ./...
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.24' }
      - run: go build ./cmd/pali
```

### GitHub Actions — Release Pipeline
- [ ] Create `.github/workflows/release.yml`:
  - Trigger: push of tag `v*`
  - Jobs:
    - Run full test suite
    - Build binaries for linux/amd64, linux/arm64, darwin/arm64, windows/amd64 via `GOOS`/`GOARCH`
    - Create GitHub Release with `gh release create` and attach binaries
    - Build and push Docker image to GHCR (`ghcr.io/pali-mem/pali`)

```bash
# Manually trigger a release (after workflows exist)
git tag v0.1.0
git push origin v0.1.0
# GitHub Actions picks it up automatically
```

### GitHub Actions — Python SDK (when ready)
- [ ] Create `.github/workflows/python-sdk.yml`:
  - Trigger: push to `python/` directory or tag `py-v*`
  - Jobs: `pip install`, `pytest`, `twine upload` to PyPI
  - Store PyPI token as `PYPI_TOKEN` repo secret:
    ```bash
    gh secret set PYPI_TOKEN --body "your-token-here"
    ```

### PR Management
- [ ] Create `.github/PULL_REQUEST_TEMPLATE.md` — checklist: tests added, docs updated, changelog entry
- [ ] Create `.github/ISSUE_TEMPLATE/bug_report.yml` — structured bug form
- [ ] Create `.github/ISSUE_TEMPLATE/feature_request.yml` — structured feature form
- [ ] Enable GitHub auto-merge for PRs that pass CI:
  ```bash
  gh repo edit --enable-auto-merge
  ```
- [ ] Add `stale` action — auto-label issues inactive for 30 days, close after 60:
  - Create `.github/workflows/stale.yml` using `actions/stale@v9`

### Secrets to Configure
```bash
# Docker / GHCR (for release pipeline)
gh secret set GHCR_TOKEN --body "your-ghcr-pat"

# AWS deploy (for scripts/deploy-aws.sh)
gh secret set AWS_ACCESS_KEY_ID --body "..."
gh secret set AWS_SECRET_ACCESS_KEY --body "..."
gh secret set AWS_REGION --body "us-east-1"

# PyPI (when Python SDK is ready)
gh secret set PYPI_TOKEN --body "..."
```

### Dependabot
- [ ] Create `.github/dependabot.yml`:
```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: { interval: weekly }
  - package-ecosystem: pip
    directory: /python
    schedule: { interval: weekly }
  - package-ecosystem: github-actions
    directory: /
    schedule: { interval: weekly }
```

### Labels (run once)
```bash
# Create standard label set
gh label create "bug" --color "d73a4a"
gh label create "enhancement" --color "a2eeef"
gh label create "good first issue" --color "7057ff"
gh label create "help wanted" --color "008672"
gh label create "breaking change" --color "e4e669"
gh label create "python-sdk" --color "3572A5"
gh label create "retrieval" --color "0075ca"
gh label create "infrastructure" --color "e4e669"
```

---

## Release Gate Command

Before tagging any release:

```bash
make test && make test-integration && make test-e2e && make build
```

All must pass green.

### Tagging a release
```bash
# Ensure you're on main and clean
git checkout main
git pull origin main

# Tag and push — triggers release workflow
git tag v0.1.0 -m "v0.1.0: initial public release"
git push origin v0.1.0

# Verify release was created
gh release view v0.1.0
```
