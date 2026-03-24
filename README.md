<div align="center">

# Pali

![Go Version](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go)
![License](https://img.shields.io/badge/License-MIT-green.svg)
![Status](https://img.shields.io/badge/Status-v0.2.0-blue)

<a href="https://github.com/user-attachments/assets/704a5235-4782-4d50-bdc0-8e929ba1c8c3">
  <img src="pali_banner.png" alt="Pali" width="830" />
</a>

*Open memory runtime for LLMs and agents.*

</div>

Pali is a local-first inspectable memory runtime for LLM apps and agent systems.

![Pali dashboard demo](https://raw.githubusercontent.com/pali-mem/.github/main/dashboard.gif)

## Quickstart

Install:

```bash
curl -fsSL https://raw.githubusercontent.com/pali-mem/pali/main/scripts/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/pali-mem/pali/main/scripts/install.ps1 | iex
```

Run:

```bash
pali init
pali serve
```

Check health:

```bash
curl http://127.0.0.1:8080/health
```

Open the dashboard:

[http://127.0.0.1:8080/dashboard](http://127.0.0.1:8080/dashboard)

Source checkout:

```bash
git clone https://github.com/pali-mem/pali.git
cd pali
make setup
make run
```

## Common Commands

```bash
pali init
pali serve
pali mcp serve
```

`pali init` creates `pali.yaml` when it is missing and prepares the runtime directories and prerequisites for the current config.

Useful flags:

```bash
pali init -config /path/to/pali.yaml
pali init -download-model
```

## Docker

If you prefer a container first run:

```bash
docker build -t pali:local .
docker run --rm -p 8080:8080 -v pali-data:/var/lib/pali pali:local
```

By default, `auth.enabled` is `false`; when you enable it, set `auth.jwt_secret` in `pali.yaml` and keep JWTs tenant-scoped.

## What Pali Does

- Stores memories with tenant isolation
- Retrieves with lexical and vector search
- Ranks and fuses candidates before returning results
- Exposes REST, MCP, and dashboard surfaces
- Supports SQLite, Qdrant, and Neo4j backends where configured
- Uses configurable embedding and scoring providers

## Architecture

![Pali runtime design](design.png)

<details>
<summary>Show architecture details</summary>

- Inputs come from LLM apps, agents, MCP hosts, and API clients
- The core memory service handles tenant isolation, search, fusion, and post-processing
- Results are exposed through the REST API, MCP server, and dashboard
- Storage and extensions plug in through SQLite metadata, vector stores, graph stores, embeddings, and scoring

</details>

## Docs

Start here:

- [Pali Docs home](https://pali-mem.github.io/pali/)
- [Getting Started](https://pali-mem.github.io/pali/getting-started/)
- [Quick Reference](https://pali-mem.github.io/pali/quick-reference/)
- [Operations Checklist](https://pali-mem.github.io/pali/operations-checklist/)

Common guides:

- [Configuration](https://pali-mem.github.io/pali/configuration/)
- [Deployment](https://pali-mem.github.io/pali/deployment/)
- [Operations](https://pali-mem.github.io/pali/operations/)
- [API](https://pali-mem.github.io/pali/api/)
- [MCP Integration](https://pali-mem.github.io/pali/mcp/)
- [Architecture](https://pali-mem.github.io/pali/architecture/)

Contributors should start with the docs site and keep [`docs/README.md`](docs/README.md) open as the local map.

## SDKs

- Go client: [`docs/client/README.md`](docs/client/README.md)
- Python SDK repo: [pali-mem/pali-py](https://github.com/pali-mem/pali-py)
- Python package: [pali-client on PyPI](https://pypi.org/project/pali-client/)
- JavaScript SDK repo: [pali-mem/pali-js](https://github.com/pali-mem/pali-js)
- JavaScript package: [pali-client on npm](https://www.npmjs.com/package/pali-client)

## Build

```bash
make build
```

## AI Disclosure

OpenAI Codex was used as an assistant during the creation and editing of Pali. Final product decisions, review, and release ownership remain with the project maintainers.
