# 2026-03-04: MCP Tenant Resolution Fallback + Capabilities Tool

## Summary

Updated MCP server/tooling to reduce tenant-ID friction for AI tool calls while preserving explicit multi-tenant behavior.

Changes:

- Added `default_tenant_id` config support (`internal/config`, `pali.yaml.example`).
- Added MCP tool `pali_capabilities` that returns canonical tool names and example calls.
- Made `tenant_id` optional on tenant-aware MCP tools:
  - `memory_store`
  - `memory_store_preference`
  - `memory_search`
  - `memory_list`
  - `memory_delete`
  - `tenant_stats`
  - `tenant_exists`
- Implemented one centralized tenant resolver in MCP tool layer with this fallback order:
  1. explicit `tenant_id` argument
  2. JWT tenant claim (when auth is enabled and token metadata is present)
  3. MCP session default tenant
  4. `default_tenant_id` from config
  5. clear `invalid input` error
- Added tenant-resolution logging (`source`, `tenant_id`, `tool`, `session_id`) for debugging/audit.

## Why

Tool-calling models often miss infrastructure details like tenant IDs. Requiring `tenant_id` in every MCP call caused avoidable failures and brittle prompt hacks.

This keeps strict explicit tenant support while adding practical fallback behavior for local single-user flows.

## Validation

Automated tests added in `internal/mcp/server_test.go` for:

- tool registry includes `pali_capabilities`
- fallback to `default_tenant_id` when `tenant_id` is omitted
- session tenant reuse after an explicit tenant call
- clear error when tenant cannot be resolved

Command run:

```bash
go test ./internal/mcp ./internal/config
```

Result:

- `ok   github.com/vein05/pali/internal/mcp`
- `ok   github.com/vein05/pali/internal/config`

## Notes

- JWT tenant fallback depends on MCP transport/token verifier populating token metadata (`TokenInfo.Extra`).
- For strict multi-user production, leave `default_tenant_id` empty and rely on explicit/JWT tenant scoping.
