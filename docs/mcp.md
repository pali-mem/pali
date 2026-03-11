# MCP Integration

Pali ships an MCP server over `stdio`, launched from the unified CLI at [`cmd/pali/main.go`](../cmd/pali/main.go) using `pali mcp run`.

## Runtime Wiring

Server implementation:
- [`internal/mcp/server.go`](../internal/mcp/server.go)
- [`internal/mcp/tools/registry.go`](../internal/mcp/tools/registry.go)

Startup path (`pali mcp run`):
1. Load config (`pali.yaml`)
2. Open SQLite and run migrations
3. Build services:
   - memory service (repo + vectorstore + embedder + scorer)
   - tenant service (repo)
4. Create MCP server and register tools
5. Run via `stdio` transport

Production command pattern:
- `pali mcp run -config /etc/pali/pali.yaml`
- This is the stable command to reference from MCP hosts (Claude Desktop/Cursor/etc.).
- Config reference: [`docs/configuration.md`](configuration.md)

## Built-In Agent Guidance

Pali exposes memory-first guidance through standard MCP surfaces so hosts can adopt defaults without per-user instruction files:

- `initialize.instructions`: tells agents to call `memory_search` before history-dependent answers and write durable facts with `memory_store`/`memory_store_preference`.
- prompt `pali_memory_autopilot`: a reusable prompt snippet hosts can inject automatically for memory-first behavior.

Note: this improves default tool usage, but MCP servers cannot force tool calls when the host ignores prompts/instructions.

## Tool Catalog (11 common tools)

1. `memory_store`
2. `memory_store_preference`
3. `memory_search`
4. `memory_list`
5. `memory_delete`
6. `tenant_create`
7. `tenant_list`
8. `tenant_stats`
9. `tenant_exists`
10. `health_check`
11. `pali_capabilities`

All tools return structured output and use MCP `isError` for tool-level failures.

Memory tool highlights:
- `memory_store` supports optional provenance fields: `source`, `created_by` (`auto|user|system`)
- `memory_search` supports optional retrieval filters: `min_score` (`0..1`) and `tiers` (`working|episodic|semantic`)
- memory item outputs include `source`, `created_by`, `recall_count`, `last_accessed_at`, `last_recalled_at`

Tenant-aware tools (`memory_*`, `tenant_stats`, `tenant_exists`) resolve tenant in this order:

1. `tenant_id` in tool input (explicit)
2. JWT tenant claim (when auth is enabled and token metadata is available)
3. MCP session default tenant (if available)
4. `default_tenant_id` from config
5. else tool returns clear `isError=true`

This is a resolution contract, not a guarantee that every MCP host is forwarding auth metadata the same way. If your deployment requires strict tenant-bound bearer auth, validate the REST API path separately and treat MCP host integration as an additional trust boundary.

## Protocol Layout Used

Pali follows the standard MCP flow:
- `initialize`
- `prompts/list`
- `prompts/get`
- `tools/list`
- `tools/call`

Tool input schemas are derived from typed Go structs in `internal/mcp/tools/registry.go`.

## Automated Testing

Code tests:
- [`internal/mcp/server_test.go`](../internal/mcp/server_test.go)
  - validates tool registry contains expected tools
  - runs end-to-end tool calls via in-memory MCP transports
  - executes tenant + memory flow (`tenant_create`, `memory_store`, `memory_search`)

Run:

```bash
go test ./internal/mcp -v
```

## Human Testing Checklist

1. Start MCP server:
   - `go run ./cmd/pali mcp run -config pali.yaml`
2. Connect an MCP client (Claude Desktop / Cursor / another MCP host) to the `pali mcp run` stdio command.
3. Verify `tools/list` returns the 11 tools above.
4. Call:
   - `pali_capabilities`
   - `tenant_create`
   - `memory_store`
   - `memory_search`
   - `tenant_stats`
5. Validate error behavior:
   - `memory_store` with missing `tenant_id` and no fallback configured -> tool `isError=true`
   - `memory_delete` wrong tenant -> tool `isError=true`
6. Confirm memory lifecycle:
   - store memory
   - search memory
   - delete memory
   - search again returns empty
