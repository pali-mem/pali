# Observational Memory Mode (design proposal, 2026-10-25)

Status: proposal, not implemented. Open questions at the end need a maintainer decision before building.

## Why

Pali's retrieval recall is the measured bottleneck: on LoCoMo the right evidence is in the top 10 under half
the time, and multi-hop answers are near zero (see `docs/benchmarks.md` and `scope/` notes). Systems that score
highest on LongMemEval in 2026 (Mastra Observational Memory, 84-95% depending on reader model) do not rely on
retrieval at all for the bulk of context: an observer model compresses conversation history into short, dated
observations, a reflector condenses them when they grow, and the whole log is served as one stable,
cacheable context block.

Pali already has the pieces this needs: per-tenant storage, an `observation` memory kind, and a leased,
retried async job queue (`internal/core/memory/postprocess_worker.go`). This mode adds two job types and one
read endpoint. It does not replace hybrid search; it is an additional way to read a tenant's memory.

## Goals

- A tenant can opt in; when on, raw turns are periodically compressed into dated observations by an LLM.
- Observations are condensed (reflected) when their total size passes a threshold, so the log stays bounded.
- One call returns the tenant's current observation log as a single text block, stable between updates so
  clients can prompt-cache it.
- Fully inspectable: every observation links back to the raw turns it came from; jobs show in `/v1/memory/jobs`.

## Non-goals

- No change to `/v1/memory/search` ranking.
- No new storage backend. SQLite rows only; vector backends are untouched.
- Not on by default. Zero-dependency first boot stays zero-dependency.

## Design

### Data

- Observations are ordinary `memories` rows with `kind = observation`, `created_by = system`,
  `source = "observer"` or `"reflector"`, and metadata:
  - `observed_turn_ids`: raw-turn memory IDs covered (observer) or observation IDs replaced (reflector)
  - `observed_from` / `observed_to`: time range of the covered turns
  - `generation`: 0 for observer output, incremented by each reflection
- Superseded observations are not deleted; they get `metadata.superseded_by = <id>` and are excluded from the
  log. This keeps the audit trail the dashboard can show.
- Per-tenant watermark: last raw-turn `created_at` that has been observed. Stored in a new
  `tenant_observation_state` table (`tenant_id`, `observed_until`, `log_tokens`, `updated_at`).

### Jobs

Two new `PostprocessJobType` values, executed by the existing worker loop:

- `observe`: enqueued after a store when the tenant has observational mode on and unobserved raw-turn tokens
  exceed `observe_threshold_tokens`. Reads unobserved turns in time order, calls the observer model, writes
  observations, advances the watermark in the same transaction.
- `reflect`: enqueued after `observe` when the active log exceeds `reflect_threshold_tokens`. Reads active
  observations, calls the reflector model, writes condensed observations, marks inputs superseded.

At most one queued or running `observe`/`reflect` job per tenant (dedupe on enqueue), so concurrent stores do
not fan out duplicate LLM calls.

### Read API

- `GET /v1/memory/observations?tenant_id=` returns `{ "log": "<text block>", "updated_at": ..., "tokens": n,
  "observation_count": n }`. The log is active observations ordered by `observed_from`, one dated bullet each.
  Deterministic formatting so identical state yields identical bytes (prompt-cache friendly).
- MCP tool `memory_observations` returning the same block.

### Config

```yaml
observational_memory:
  enabled: false
  provider: openrouter        # openrouter | ollama
  observer_model: ""          # required when enabled
  reflector_model: ""         # defaults to observer_model
  observe_threshold_tokens: 2000
  reflect_threshold_tokens: 30000
```

Token counts use a cheap estimate (characters / 4) rather than a tokenizer dependency.

### Prompts

Observer: "Rewrite these conversation turns as short, dated, third-person observations. Keep names, dates,
numbers, preferences, decisions, and plans. Drop chit-chat. One observation per line, prefixed with the date."
Reflector: "Merge these observations. Keep the most recent value when facts conflict, keep dates, remove
duplicates, keep it under N tokens." Prompts live next to the existing parser prompts and are versioned in
the observation metadata (`extractor_version`) so a prompt change is visible in stored rows.

## Testing

- Unit: job enqueue thresholds, dedupe, watermark advance, superseded exclusion, deterministic log formatting.
- Integration: fake LLM client returning canned observations; store N turns, run workers, assert the log.
- Evaluation: this mode is only worth shipping if it measurably helps. Run the LongMemEval-S protocol in
  `docs/internal/longmemeval-protocol.md` with the reader given (a) top-k search results and (b) the
  observation log, same reader and judge.

## Open questions (maintainer decision)

1. Which observer model to default to in docs (cost vs quality); is OpenRouter-only acceptable for v1?
2. Should observational mode be per tenant (stored setting, needs the tenant settings table from
   `per-tenant-scoring-design.md`) or global config only for v1? Global is less work.
3. Should the observation log also be searchable via `/v1/memory/search` (it is, by default, since
   observations are memories), or excluded to keep search results unchanged?
