# Unreleased

Working notes for the next release. Fold into `CHANGELOG.md` at tag time.

## Added

- REST: `GET /v1/memory/:id`, `GET /v1/memory` (offset pagination with `next_offset`, optional `tier` and `kind`
  filters), and `PATCH /v1/memory/:id` (partial update of `content`, `tags`, `tier`, `importance`). A content change
  rewrites the FTS entry, clears the query view generated from the old content, and re-embeds into the vector store.
- REST: `GET /v1/tenants` with per-tenant memory counts. A tenant-scoped JWT sees only its own tenant.
- REST: `GET /v1/tenants/:id/stats` also returns `by_tier`, `by_kind`, `total_recalls`, and `newest_memory_at`.
  `memory_count` is unchanged.
- Memory responses (including search) now include `importance`.
- Go SDK: `GetMemory`, `ListMemory`, `UpdateMemory`, `ListTenants`, and the extended stats fields.
- MCP: `memory_get` and `memory_update` tools.
- `pali connect <claude-code|cursor|codex>` adds the Pali MCP server to the agent's project or user config
  (idempotent, `--dry-run` prints a diff, atomic writes).
- `pali mcp serve -tenant <id|auto>` sets the default tenant and creates it on startup; `auto` derives it from the
  git repository or directory name.
- Claude Code plugin in `integrations/claude-code/` with MCP registration and a SessionStart memory protocol.
- Docs: coding-agent setup guide, benchmarks methodology page, design proposals for observational memory,
  a LongMemEval-S protocol, and per-tenant scoring weights (`docs/internal/`).
- Test that fails when a `Config` field is never read outside `internal/config`.

## Changed

- Parser: `parser.max_facts` is honoured. `0` (the new default) keeps the previous effective behaviour: the
  heuristic parser keeps 1 fact beside the raw turn (2 without it) and LLM parsers keep up to 4. Positive values are
  the number of parsed facts kept per turn. Configs that set `max_facts` above 1 with the heuristic parser now keep
  more facts; the checked-in benchmark profiles and LoCoMo scripts were set to `0` to keep their behaviour.
- Retrieval: entity-hint, graph-bridge, and aggregation searches look up entity facts in one batched repository call
  (SQLite and Neo4j) instead of one query per entity/relation pair. On LoCoMo-500 this cuts entity-fact lookups from
  about 19 to about 1.2 per search. Rankings are unchanged (per-query diff over 6,468 searches).
- Internal: `SearchWithFilters` split into named stages; duplicated comparator, filter, and lookup-limit helpers
  consolidated. No ranking change.

## Fixed

- Parser: no longer invents the entity `"user"`. When the speaker is unknown the entity fact is skipped (the memory is
  still stored); for annotated turns the speaker is used. Existing `"user"` rows are not migrated.
- Parser: fact values are no longer filled with the rest of the sentence, so query-view text and embeddings no longer
  contain those fragments. Side effect: unresolved first-person facts get no query view.
- Rerank: an explicit `RerankOptions.Blend` of 0 is no longer replaced by the 0.40 default; values are clamped to
  [0, 1]. Defaults are unchanged.

## Removed

- `instructions.md`; its tier-resolution rules now live in `docs/architecture.md`.
