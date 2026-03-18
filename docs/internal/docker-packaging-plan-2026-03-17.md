# Docker Packaging Plan

Date: 2026-03-17

## Recommendation

Pali should ship:

1. one official app image for the core server
2. one-command Docker Compose stacks for anything that needs extra services
3. production docs that treat Qdrant, Neo4j, and Ollama as separate containers or managed services

The default user experience should be:

- `docker run` for the zero-dependency lexical + SQLite profile
- `docker compose up` for "run the whole stack for me"

It should not default to "everything inside one container" once external stores or model runtimes are involved. That is not how infra products like this are usually packaged, and it creates upgrade, health, storage, and security problems that Compose solves more cleanly.

## Why This Fits Pali

Pali already has a clean zero-dependency path:

- `vector_backend: sqlite`
- `entity_fact_backend: sqlite`
- `embedding.provider: lexical`

That makes a single official image practical for the base product.

Pali also already has optional external dependencies:

- Qdrant
- Neo4j
- Ollama
- OpenRouter

Those are better treated as networked dependencies than subprocesses hidden inside one monolithic container.

## Current Repo Gaps

The repo is close, but not container-friendly out of the box yet:

- default bind host is `127.0.0.1`, which is wrong for container networking
- default SQLite path is relative (`file:pali.db?cache=shared`)
- default ONNX model paths are relative (`./models/...`)
- static assets are served from a relative path (`./web/static`)
- config loading is YAML-first with only two env fallbacks today
- there is no Dockerfile, `.dockerignore`, Compose stack, or image publishing workflow
- API startup uses `router.Run(...)` directly, so there is no explicit graceful SIGTERM shutdown path yet

None of these are major blockers. They just mean Docker support should be introduced as a deliberate packaging slice, not as a thin wrapper around the current local-dev defaults.

## What Comparable Infra Usually Does

The pattern across comparable self-hosted infra is consistent:

- ship one official image for the app
- mount config and persistent data into the container
- use environment variables for container-friendly overrides
- add health checks
- publish Compose for local or low-scale deployment
- use Helm/Terraform or platform-native deployment for production
- keep databases, vector stores, caches, and model runtimes as separate services

Examples from official docs:

- Qdrant documents an official container image, mounted config files, environment-variable overrides, and startup config validation: <https://qdrant.tech/documentation/guides/configuration/>
- Weaviate documents Docker and Kubernetes configuration through environment variables: <https://docs.weaviate.io/deploy/configuration/env-vars>
- Chroma documents Docker Compose health checks and dependency-aware deployments: <https://cookbook.chromadb.dev/running/health-checks/>
- Langfuse explicitly positions Docker Compose for low-scale deployments and Helm/Terraform for production-scale self-hosting, with separate app and storage components: <https://langfuse.com/self-hosting>
- Docker recommends multi-stage builds for small runtime images: <https://docs.docker.com/build/building/multi-stage/>
- Docker's own guidance flags running as a non-root default user as the safer runtime posture: <https://docs.docker.com/scout/policy/>
- Docker Compose supports startup ordering based on `service_healthy`, which is the right fit for Pali plus optional backing services: <https://docs.docker.com/compose/how-tos/startup-order/>
- GitHub already documents the release workflow pattern for publishing container images to GHCR and/or Docker Hub: <https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images>

## Recommended Packaging Model

### Mode 1: Single-container base runtime

Use this for the simplest pull-and-run experience.

Contents:

- Pali binary
- default container config
- static web assets
- optional empty data directory

Backends:

- SQLite
- lexical embeddings

Expected command shape:

```bash
docker run --name pali \
  -p 8080:8080 \
  -v pali-data:/var/lib/pali \
  ghcr.io/pali-mem/pali:v0.1.0
```

This should be the official "easy start" path.

### Mode 2: One-command multi-service stack

Use Docker Compose for everything beyond the base profile.

Recommended profiles:

- `base`: Pali + persistent volume
- `qdrant`: adds Qdrant
- `neo4j`: adds Neo4j
- `ollama`: adds Ollama
- `full`: combines the above where practical

That gives users one command without pretending the system is actually one process:

```bash
docker compose --profile qdrant --profile neo4j up -d
```

This is the usual compromise infra teams make: one command, multiple containers.

### Mode 3: Production deployment

For production, treat Pali as one application image deployed:

- behind a reverse proxy or ingress
- with persistent storage
- with externalized secrets
- with managed or separately-operated Qdrant/Neo4j/Ollama where used

That can live on:

- Docker Compose on a VM for small installs
- Kubernetes for larger installs
- Nomad/systemd if the operator prefers that style

## Recommended Repo Changes

### Phase 1: Container-readiness hardening

1. Add a container-specific default config file with:
   - `server.host: 0.0.0.0`
   - absolute in-container paths such as `/var/lib/pali/pali.db`
   - service-name URLs such as `http://qdrant:6333`, `bolt://neo4j:7687`, and `http://ollama:11434`
2. Add broad env-based config overrides instead of only:
   - `OPENROUTER_API_KEY`
   - `NEO4J_PASSWORD`
3. Add explicit graceful shutdown for API mode so container stop behaves cleanly.
4. Keep the current local-dev defaults intact; do not force container defaults onto non-container users.

### Phase 2: Official image

Add:

- `Dockerfile`
- `.dockerignore`
- optional `docker/entrypoint.sh` only if config templating is needed

Runtime requirements:

- multi-stage build
- small runtime image
- non-root user
- OCI labels
- exposed `8080`
- built-in health check against `/health`

The image should contain:

- `/app/pali`
- `/etc/pali/pali.yaml`
- `/app/web/static`

Recommended runtime defaults:

- `WORKDIR /app`
- `ENTRYPOINT ["/app/pali"]`
- `CMD ["-config", "/etc/pali/pali.yaml"]`

### Phase 3: Compose stacks

Add a `deploy/docker/` area with:

- `compose.yaml`
- `compose.qdrant.yaml` or profile-based equivalents
- `compose.neo4j.yaml` or profile-based equivalents
- `compose.ollama.yaml` or profile-based equivalents
- sample env file
- sample container config(s)

The `pali` service should:

- mount persistent storage
- depend on backing services with `service_healthy` where possible
- pass secrets by env
- default to the base lexical + SQLite profile when no extra services are enabled

### Phase 4: Release automation

Extend release automation to publish:

- `ghcr.io/pali-mem/pali:<version>`
- `ghcr.io/pali-mem/pali:latest` after stable release policy exists
- `linux/amd64` and `linux/arm64`

Optional later mirror:

- Docker Hub, if discoverability matters enough to justify another registry

Also add:

- SBOM/provenance attestations if buildx is introduced
- image smoke test in CI

### Phase 5: Docs and support matrix

Update docs to clearly separate:

- local source run
- single-container base runtime
- Compose stacks
- production deployment

Document supported combinations explicitly:

| Profile | Packaging | Supported? | Notes |
|---|---|---:|---|
| SQLite + lexical | single container | yes | default and easiest pull/run path |
| SQLite + ONNX | single container | yes, later | best as mounted model files or separate image variant |
| SQLite + OpenRouter | single container | yes | env-based secret injection |
| Qdrant + lexical/openrouter | Compose or external service | yes | do not embed Qdrant in the Pali image |
| Neo4j graph mode | Compose or external service | yes | keep Neo4j separate |
| Ollama-backed embedding/parser/scorer | Compose or external service | yes | keep Ollama separate due to model/runtime weight |
| Qdrant + Neo4j + Ollama all in one image | single container | no | high-friction demo hack, not a real deployment model |

## Specific Product Decisions

### 1. Prefer "single command" over "single container"

For Pali, "easy to run everything" should mean:

- `docker compose up`

not:

- one huge image that bundles Qdrant, Neo4j, and Ollama

The first is maintainable. The second becomes brittle quickly.

### 2. GHCR first, Docker Hub second

Because the repo already ships through GitHub Actions and GitHub Releases, GHCR is the cleanest first registry. Add Docker Hub only if users clearly expect it.

### 3. Keep the main image lean

Do not bake heavy ONNX models or Ollama models into the default image.

If needed later:

- support mounted ONNX assets
- or publish an explicit optional variant such as `pali:onnx`

### 4. Keep config file support

Do not force everything through env vars. Infra operators still want a real config artifact.

The right model is:

- baked container default config
- mounted custom config for real deployments
- env overrides for secrets and simple overrides

## Concrete Implementation Order

If this becomes an implementation sprint, the order should be:

1. container config + env override design
2. graceful shutdown
3. Dockerfile + `.dockerignore`
4. local `docker run` smoke test
5. Compose base stack
6. Compose profiles for Qdrant/Neo4j/Ollama
7. CI image build + smoke test
8. release workflow image publishing
9. public docs

## Short Answer

Yes, Pali should be Docker-friendly.

But the right shape is:

- one official Pali image for the base runtime
- one-command Compose stacks for non-base setups
- not a single giant container for every dependency

That matches how infra like this is usually packaged, and it fits Pali's current architecture much better than an all-in-one container.
