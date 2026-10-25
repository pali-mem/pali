# Use with Coding Agents

Pali runs as an MCP server over stdio, so Claude Code, Cursor and Codex can use it as long-term memory. `pali connect` writes the server entry into each agent's config for you.

## Prerequisites

1. Install the `pali` binary (see [Getting Started](getting-started.md)) and make sure it is on a stable path. `pali connect` records the absolute path of the binary you run it with, so re-run it after moving or reinstalling `pali`. A binary started with `go run` has a temporary path; build one first with `go build -o pali ./cmd/pali`.
2. Optional: `pali init` creates a `pali.yaml`. Without `--config`, the MCP server reads `pali.yaml` from the agent's working directory, so pass `--config <path>` when the file lives elsewhere. See [Configuration](configuration.md).

## Per-project tenants

Each project gets its own tenant, so memories from different repositories do not mix.

- `pali connect` in project scope derives the tenant ID from the git repository (or directory) name: the name is lowercased and runs of characters outside `a-z`, `0-9` and `_` become `-`. `My Repo` becomes `my-repo`.
- Override it with `--tenant <id>`.
- In user scope with no `--tenant`, the entry passes `-tenant auto`, and the tenant is derived at startup from the working directory the agent launches the server in. This relies on the agent starting the server in the project directory; if it does not, pass `--tenant`.
- `pali mcp serve -tenant <id>` sets the default tenant for that server (it takes precedence over `default_tenant_id` in config and `PALI_DEFAULT_TENANT_ID`) and creates the tenant if it does not exist. `-tenant auto` derives the ID as described above.

Two repositories with the same name resolve to the same tenant. Pass `--tenant` to separate them.

## Connect an agent

```bash
pali connect claude-code            # writes .mcp.json in the project root
pali connect cursor                 # writes .cursor/mcp.json
pali connect codex                  # writes .codex/config.toml
```

Flags:

| Flag | Meaning |
| --- | --- |
| `--scope project\|user` | `project` (default) writes into the git repository root (or the current directory outside a repository). `user` writes the agent's global config. |
| `--tenant <id>` | Tenant ID. Default: see above. |
| `--config <path>` | Pali config file for the server; stored as an absolute path. |
| `--dry-run` | Print a diff of what would change and write nothing. |

The command is idempotent: running it again with the same options reports `already configured`. Other servers and settings in the file are kept. Only the `pali` entry is added or replaced. JSON files are rewritten with 2-space indentation and sorted keys. In `config.toml`, only the `[mcp_servers.pali]` table (and sub-tables such as `[mcp_servers.pali.env]`) is replaced; other tables and comments are untouched. Files are written atomically (temp file and rename). Run with `--dry-run` first to review the change.

The entry runs the absolute path of the binary:

```text
/abs/path/to/pali mcp serve [-config /abs/path/pali.yaml] -tenant <id>
```

### Claude Code

| Scope | File |
| --- | --- |
| project | `.mcp.json` in the project root |
| user | `~/.claude.json` (top-level `mcpServers`) |

```bash
pali connect claude-code --dry-run
pali connect claude-code
```

Claude Code asks you to approve project-scoped servers on first use. The user-scope file `~/.claude.json` is also rewritten by Claude Code itself; close running sessions before using `--scope user` to avoid a concurrent write. Verify with `claude mcp list` or `/mcp` inside a session.

Format reference: [Claude Code MCP docs](https://code.claude.com/docs/en/mcp).

### Cursor

| Scope | File |
| --- | --- |
| project | `.cursor/mcp.json` |
| user | `~/.cursor/mcp.json` |

```bash
pali connect cursor
```

Restart Cursor or reload MCP servers, then check Settings, MCP. Format reference: [Cursor MCP docs](https://cursor.com/docs/context/mcp).

### Codex CLI

| Scope | File |
| --- | --- |
| project | `.codex/config.toml` (Codex reads project config only for trusted projects) |
| user | `~/.codex/config.toml` |

```bash
pali connect codex --scope user
```

The result:

```toml
[mcp_servers.pali]
command = "/abs/path/to/pali"
args = ["mcp", "serve", "-tenant", "my-repo"]
```

Format reference: [Codex MCP docs](https://developers.openai.com/codex/mcp).

## Claude Code plugin

The plugin in [`integrations/claude-code`](https://github.com/pali-mem/pali/tree/main/integrations/claude-code) bundles three things:

- the Pali MCP server, started as `pali mcp serve -config "$PALI_CONFIG" -tenant auto` (one tenant per project, derived from the project directory),
- a database in the plugin data directory, so memory persists across plugin updates and is shared across projects (tenants keep them apart),
- a `SessionStart` hook that adds a short memory protocol to each session.

The plugin needs `pali` on your `PATH`. Try it from a checkout:

```bash
claude --plugin-dir /path/to/pali/integrations/claude-code
```

Set `PALI_CONFIG` to a config file path to use your own settings; when it is empty the server runs on built-in defaults plus `PALI_*` environment variables. Confirm the server with `/mcp` (it appears as `plugin:pali-memory:pali`).

The protocol the hook injects:

```text
Pali memory is connected to this session through the pali MCP server, scoped to this project's tenant.
Memory protocol for this session:
- Before answering questions about earlier decisions, user preferences, or project facts, call memory_search with the question as the query.
- When a durable decision, preference, convention, or project fact is settled, call memory_store (memory_store_preference for user preferences) with one self-contained sentence.
- Never store secrets: API keys, tokens, passwords, credentials, or private personal data.
- Do not store transient task state, raw logs, or content already in the repository.
- If memory_search returns nothing relevant, say so and continue; do not invent remembered facts.
```

The hook prints the file with `cat`, so it works on macOS and Linux; on Windows use `pali connect claude-code` instead. Plugin format reference: [Claude Code plugins](https://code.claude.com/docs/en/plugins-reference).

Cursor and Codex have no equivalent session hook here. The MCP server's own `initialize.instructions` and the `pali_memory_autopilot` prompt carry the same guidance (see [MCP Integration](mcp.md)); to make it stricter, paste the protocol above into `AGENTS.md` or a Cursor rule.

## Troubleshooting

- `ERROR: ... parse existing JSON`: the target file is not valid JSON (some editors allow comments). `pali connect` leaves it untouched; fix the file and re-run.
- The agent reports the server failed to start: run the command from the entry by hand; a missing `pali.yaml` in the working directory is the usual cause, so pass `--config`.
- Memory lands in the wrong tenant: check the `-tenant` value in the entry, or call `tenant_list`.
