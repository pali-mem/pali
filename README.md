<div align="center">

# Pali

[![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)
[![Status](https://img.shields.io/badge/Status-v0.1-blue)](README.md)

<a href="https://github.com/user-attachments/assets/704a5235-4782-4d50-bdc0-8e929ba1c8c3">
  <img src="pali_banner.png" alt="Pali" width="830" />
</a>

*Open memory for your LLM.*

</div>

> **Pre-release, close to usable**

Pali is open memory infrastructure for LLM and agent systems. It gives you a local API server, MCP server, operator dashboard, and configurable retrieval stack behind one runtime.

## Demo

![Pali dashboard demo](https://raw.githubusercontent.com/pali-mem/.github/main/dashboard.gif)

## Quickstart

### Install on macOS or Linux

```bash
curl -fsSL https://raw.githubusercontent.com/pali-mem/pali/main/scripts/install.sh | sh
```

### Install on Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/pali-mem/pali/main/scripts/install.ps1 | iex
```

### Initialize and run

```bash
pali init
pali serve
```

Health:

```bash
curl http://127.0.0.1:8080/health
```

Dashboard:

```bash
open http://127.0.0.1:8080/dashboard
```

If you prefer a source checkout instead of a release binary:

```bash
git clone https://github.com/pali-mem/pali.git
cd pali
make setup
make run
```

## Docs Center

Start here for the full guides:

- Published docs: [https://pali-mem.github.io/pali/](https://pali-mem.github.io/pali/)
- Local docs map: [`docs/README.md`](docs/README.md)
- Getting started: [`docs/getting-started.md`](docs/getting-started.md)
- Configuration: [`docs/configuration.md`](docs/configuration.md)
- Deployment: [`docs/deployment.md`](docs/deployment.md)
- Operations: [`docs/operations.md`](docs/operations.md)
- MCP: [`docs/mcp.md`](docs/mcp.md)
- API: [`docs/api.md`](docs/api.md)

## What Pali Does

- Multi-tenant memory APIs with tenant-scoped isolation
- Hybrid retrieval across lexical, dense, reranking, and optional multi-hop expansion
- MCP server with memory-first tools
- Operator dashboard for tenants, memories, and system state
- Configurable backends for vectors, entity facts, embeddings, and scoring

Current v0.1 core capabilities:

- Memory CRUD and batch ingest APIs
- Async post-processing pipeline with job tracking
- Lexical plus dense candidate fusion via RRF
- WMR reranking
- Tenant statistics and routing support
- Tier auto-resolution (`episodic` vs `semantic`)
- Optional JWT tenant-scoped auth

## Install and Run Options

### Release binary

The install scripts download the latest GitHub Release asset for your platform, verify checksums, and place `pali` on your machine.

Manual release downloads are also available from GitHub Releases:

- Linux/macOS archives: `pali_<version>_<os>_<arch>.tar.gz`
- Windows archives: `pali_<version>_windows_<arch>.zip`

After install:

```bash
pali init
pali serve
```

### Docker

Build:

```bash
docker build -t pali:local .
```

Run:

```bash
docker run --rm -p 8080:8080 -v pali-data:/var/lib/pali pali:local
```

The image uses `deploy/docker/pali.container.yaml`, which binds to `0.0.0.0:8080` and stores SQLite data under `/var/lib/pali`.

Compose files live under `deploy/docker/`:

```bash
docker compose -f deploy/docker/compose.yaml up --build
docker compose -f deploy/docker/compose.yaml -f deploy/docker/compose.qdrant.yaml up --build
docker compose -f deploy/docker/compose.yaml -f deploy/docker/compose.neo4j.yaml up --build
docker compose -f deploy/docker/compose.yaml -f deploy/docker/compose.ollama.yaml up --build
```

### Source checkout

Prerequisite: Go `1.25+`

```bash
git clone https://github.com/pali-mem/pali.git
cd pali
make setup
make run
```

## CLI

```bash
pali init
pali serve
pali mcp serve
```

Useful init flags:

```bash
pali init -config /path/to/pali.yaml
pali init -download-model
pali init -skip-model-download
pali init -skip-runtime-check
pali init -skip-ollama-check
```

The init flow creates `pali.yaml` from `pali.yaml.example` when missing, prepares runtime directories, and checks optional ONNX/Ollama prerequisites based on the current config.

## Config and Auth

- Config template: [`pali.yaml.example`](pali.yaml.example)
- Config guide: [`docs/configuration.md`](docs/configuration.md)
- Multitenancy and JWT auth: [`docs/multitenancy.md`](docs/multitenancy.md)
- ONNX setup notes: [`docs/onnx.md`](docs/onnx.md)

JWT example:

```yaml
auth:
  enabled: true
  jwt_secret: "change-me"
  issuer: "pali"
```

JWTs must include `tenant_id`, and `/v1` requests stay tenant-scoped.

## MCP and SDKs

MCP tools include:

- `memory_store`
- `memory_store_preference`
- `memory_search`
- `memory_list`
- `memory_delete`
- `tenant_create`
- `tenant_list`
- `tenant_stats`
- `tenant_exists`
- `health_check`
- `pali_capabilities`

Go client example:

```go
import (
  "context"
  "log"

  "github.com/pali-mem/pali/pkg/client"
)

func main() {
  c, err := client.NewClient("http://127.0.0.1:8080")
  if err != nil {
    log.Fatal(err)
  }

  if _, err := c.CreateTenant(context.Background(), client.CreateTenantRequest{
    ID:   "tenant_1",
    Name: "Tenant One",
  }); err != nil {
    log.Fatal(err)
  }
}
```

More:

- MCP docs: [`docs/mcp.md`](docs/mcp.md)
- Go client docs: [`docs/client/README.md`](docs/client/README.md)
- Python SDK repo: [`pali-mem/pali-py`](https://github.com/pali-mem/pali-py)
- Python package: [`pali-client` on PyPI](https://pypi.org/project/pali-client/)
- JavaScript SDK repo: [`pali-mem/pali-js`](https://github.com/pali-mem/pali-js)
- JavaScript package: [`pali-client` on npm](https://www.npmjs.com/package/pali-client)

Install:

```bash
pip install pali-client
npm install pali-client
```

## Build and Release

Build locally:

```bash
make build
```

Install locally from source:

```bash
make install PREFIX="$HOME/.local"
```

Tests:

- `make test`
- `make test-integration`
- `make test-e2e`
- `make test-all`

Release assets:

```bash
VERSION=v0.1.0 make release-assets
```

The release workflow at [`.github/workflows/release.yml`](.github/workflows/release.yml) publishes GitHub Release binaries and a multi-arch container image to `ghcr.io/pali-mem/pali`.

## Production Notes

- Keep `pali.yaml` outside the repo in non-dev environments.
- Enable JWT auth before exposing `/v1` outside a trusted network.
- Persist `database.sqlite_dsn` across restarts.
- Put Pali behind TLS termination and a restart supervisor.
- Monitor `/health` and validate config before deploy.

Full runbook: [`docs/operations.md`](docs/operations.md)

## Module Path

`github.com/pali-mem/pali`
