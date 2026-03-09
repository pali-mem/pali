# Pali SDK & Package Architecture

> Canonical design guide for every client library and middleware package in the Pali ecosystem.
> Any package shipped under `github.com/pali-mem/pali/pkg/` or as a standalone pip package **must** follow these rules.
> The goal is a single, coherent developer experience across Go, Python, and any future language.

---

## Table of Contents

1. [Core Philosophy](#1-core-philosophy)
2. [Package Layout Rules](#2-package-layout-rules)
3. [API Surface Design](#3-api-surface-design)
4. [Error Handling Contract](#4-error-handling-contract)
5. [Configuration & Options Pattern](#5-configuration--options-pattern)
6. [Context & Cancellation](#6-context--cancellation)
7. [Retries & Resilience](#7-retries--resilience)
8. [Testing Requirements](#8-testing-requirements)
9. [Documentation Requirements](#9-documentation-requirements)
10. [Versioning & Stability Guarantees](#10-versioning--stability-guarantees)
11. [Python-Specific Rules](#11-python-specific-rules)
12. [Go-Specific Rules](#12-go-specific-rules)
13. [Middleware Pattern](#13-middleware-pattern)
14. [Checklist (per-package)](#14-checklist-per-package)

---

## 1. Core Philosophy

These four principles drive every decision below.

### 1.1 Make the simple case trivial

A developer should reach their first working integration in under 5 minutes — zero config, zero ceremony.

```go
// Go — store a memory in 3 lines
c, _ := client.New("http://localhost:8080", client.WithBearerToken(token))
c.StoreMemory(ctx, client.StoreMemoryRequest{TenantID: "user:42", Content: "Likes jazz"})
results, _ := c.SearchMemory(ctx, client.SearchMemoryRequest{TenantID: "user:42", Query: "music"})
```

```python
# Python — same in 3 lines
c = PaliClient("http://localhost:8080", token=token)
c.store("user:42", "Likes jazz")
results = c.search("user:42", "music")
```

### 1.2 Make the hard case possible

Every knob that the API exposes must be reachable. Power users must never resort to raw HTTP.

### 1.3 Fail loudly and clearly

Wrong configuration or bad input must produce a human-readable error at the earliest possible moment — constructor, not first call.

### 1.4 Respect the caller's runtime

- Never spawn goroutines or threads the caller cannot observe or cancel.
- Never set global state.
- Never override the caller's logger, HTTP client, or retry policy unexpectedly.

---

## 2. Package Layout Rules

### 2.1 Go

```
pkg/
  client/          ← public typed HTTP client (stable)
    client.go      — New() / NewClient(), Option type
    types.go       — all request/response structs
    memory.go      — memory operations
    tenant.go      — tenant operations
    health.go      — health check
    transport.go   — doJSON, internal only
    errors.go      — APIError type + sentinel errors
    client_test.go — unit tests against httptest.Server
  middleware/      ← LLM call wrapper (stable)
    middleware.go  — Middleware struct, Wrap()
    options.go     — Option funcs
    example_test.go
```

**Rules:**
- One file per logical group of operations. No mega-files.
- `transport.go`, `errors.go` — unexported internal mechanics live here.
- All exported symbols in the package are at the top level — no sub-packages under `pkg/client/`.
- `internal/` sub-packages are forbidden inside `pkg/` — if it must be hidden, it belongs in `internal/`.

### 2.2 Python

```
pali-python/
  pali/
    __init__.py       ← exports PaliClient, PaliMiddleware, PaliError
    client.py         ← PaliClient
    middleware.py     ← PaliMiddleware
    types.py          ← dataclasses for all request/response shapes
    errors.py         ← PaliError hierarchy
    _transport.py     ← private HTTP logic (httpx)
    _retry.py         ← private retry logic
  tests/
    test_client.py
    test_middleware.py
  README.md
  pyproject.toml
```

**Rules:**
- Public symbols only in `__init__.py`. Anything prefixed `_` is private.
- `types.py` uses `dataclasses` (Python ≥ 3.10) or `pydantic` if available — no raw dicts on the public API.
- All async variants live alongside their sync counterparts in the same file (`async def search_async`).

---

## 3. API Surface Design

### 3.1 Principle of Minimal Surface

Expose the minimum set of types and functions that covers all real use cases.
Every exported symbol is a forever-promise. When in doubt, keep it unexported.

> From Google's API design guide: "A smaller API is easier to learn, easier to maintain, and harder to misuse."

### 3.2 Nouns, not verbs — but verbs for operations

| Pattern | Good | Bad |
|---------|------|-----|
| Type name | `StoreMemoryRequest` | `MemoryStoreParams` |
| Method name | `StoreMemory(ctx, req)` | `DoMemoryStore(ctx, req)` |
| Option func | `WithBearerToken(t)` | `SetToken(t)` |
| Error sentinel | `ErrNotFound` | `NotFoundError` |

### 3.3 Request structs over variadic arguments

```go
// GOOD — extensible, self-documenting
c.SearchMemory(ctx, SearchMemoryRequest{
    TenantID: "user:42",
    Query:    "music preferences",
    TopK:     5,
})

// BAD — breaks on the next added parameter
c.SearchMemory(ctx, "user:42", "music preferences", 5)
```

### 3.4 Return (value, error) — never panic

Every public operation returns an error. Panics are strictly forbidden in SDK code.

```go
// GOOD
func (c *Client) StoreMemory(ctx context.Context, req StoreMemoryRequest) (StoreMemoryResponse, error)

// BAD
func (c *Client) StoreMemory(ctx context.Context, req StoreMemoryRequest) StoreMemoryResponse // panics on error
```

### 3.5 Zero values must be safe

A zero-value request struct should either use sensible defaults or produce a clear validation error — never a panic or silent wrong behavior.

```go
// This must return a clear error, not crash or silently succeed with garbage
c.SearchMemory(ctx, SearchMemoryRequest{})
// → error: "tenant_id is required"
```

### 3.6 Immutability after construction

After `New()` / `NewClient()` returns, the client's base URL and transport are frozen. Only token can be updated via `SetBearerToken()`. This makes clients safe to share across goroutines.

---

## 4. Error Handling Contract

### 4.1 The APIError type

Every HTTP error from the Pali server is surfaced as a typed `APIError`:

```go
// errors.go
type APIError struct {
    StatusCode int    // HTTP status code
    Code       string // machine-readable code from API body, e.g. "not_found"
    Message    string // human-readable message
    RequestID  string // X-Request-ID header if present
}

func (e *APIError) Error() string {
    return fmt.Sprintf("pali: %d %s: %s (request_id=%s)", e.StatusCode, e.Code, e.Message, e.RequestID)
}
```

### 4.2 Sentinel errors for common cases

```go
var (
    ErrNotFound     = &APIError{StatusCode: 404, Code: "not_found"}
    ErrUnauthorized = &APIError{StatusCode: 401, Code: "unauthorized"}
    ErrConflict     = &APIError{StatusCode: 409, Code: "conflict"}
    ErrRateLimit    = &APIError{StatusCode: 429, Code: "rate_limited"}
)
```

Callers use `errors.Is` / `errors.As`:
```go
if errors.Is(err, client.ErrNotFound) { ... }

var apiErr *client.APIError
if errors.As(err, &apiErr) {
    log.Printf("request_id=%s", apiErr.RequestID)
}
```

### 4.3 Wrapping rule

All errors leaving the SDK are wrapped with `fmt.Errorf("pali: ...: %w", err)`.
Internal errors (JSON encoding failures, URL parsing) are wrapped separately from API errors.
**Never swallow errors silently.**

### 4.4 Validation errors are separate

```go
type ValidationError struct {
    Field   string
    Message string
}
```

Returned synchronously from operations when a required field is empty — before any network call is made.

---

## 5. Configuration & Options Pattern

### 5.1 Functional options (Go)

```go
type Option func(*Client)

func WithHTTPClient(hc *http.Client) Option
func WithBearerToken(token string) Option
func WithTimeout(d time.Duration) Option
func WithRetry(maxAttempts int, backoff RetryBackoff) Option
func WithLogger(l Logger) Option   // structured, interface-based
```

**Rules:**
- `New(baseURL, ...Option)` always has sane defaults. No option is mandatory.
- Options are additive and order-independent within reason.
- Options set via `With*` at construction time are immutable afterwards.
- Mutable post-construction state is limited to: `SetBearerToken(token string)`.

### 5.2 Keyword arguments (Python)

```python
class PaliClient:
    def __init__(
        self,
        base_url: str,
        *,                           # keyword-only after base_url
        token: str | None = None,
        timeout: float = 15.0,
        max_retries: int = 3,
        http_client: httpx.Client | None = None,
    ): ...
```

**Rules:**
- `base_url` is the only positional argument.
- All configuration is keyword-only (`*` separator).
- `None` is a valid value that means "use the default".

### 5.3 Environment variable fallbacks

| Env var | Overrides |
|---------|-----------|
| `PALI_BASE_URL` | `base_url` constructor argument |
| `PALI_TOKEN` | `token` / `WithBearerToken` |
| `PALI_TIMEOUT` | `timeout` / `WithTimeout` |

Environment variables have **lower priority** than constructor arguments. Constructor wins.

---

## 6. Context & Cancellation

### 6.1 Go

Every public method that touches the network takes `context.Context` as its **first argument**.

```go
func (c *Client) StoreMemory(ctx context.Context, req StoreMemoryRequest) (StoreMemoryResponse, error)
```

The context is passed directly to `http.NewRequestWithContext`. Callers can cancel, set deadlines, or attach trace spans.
**Never store a context in a struct field.**

### 6.2 Python

Sync methods use `httpx` with a `timeout` parameter drawn from the client config.
Async methods accept an optional `timeout` override per call:

```python
async def search_async(self, tenant_id: str, query: str, *, timeout: float | None = None) -> SearchResponse
```

---

## 7. Retries & Resilience

### 7.1 Default retry policy

| Condition | Retry? |
|-----------|--------|
| Network timeout / connection refused | Yes |
| HTTP 429 Too Many Requests | Yes — respects `Retry-After` header |
| HTTP 503 Service Unavailable | Yes |
| HTTP 4xx (except 429) | No — caller error, retrying won't help |
| HTTP 5xx (except 503) | No — server error, may not be idempotent |
| HTTP 2xx | Never |

Default: **3 attempts**, exponential backoff with **full jitter** (base 200ms, cap 5s).

### 7.2 Idempotency

`StoreMemory` is NOT idempotent by default (same content → duplicate memory). Callers must pass an idempotency key if they want deduplication:

```go
req := StoreMemoryRequest{
    TenantID:       "user:42",
    Content:        "Likes jazz",
    IdempotencyKey: "conv:abc:turn:3",  // optional; server deduplicates within 24h
}
```

Only idempotent operations (`SearchMemory`, `DeleteMemory`, all GETs) are retried by default. `StoreMemory` without an idempotency key is **never auto-retried**.

### 7.3 Disable retries

```go
c, _ := client.New(url, client.WithRetry(1, nil)) // 1 attempt = no retry
```

---

## 8. Testing Requirements

### 8.1 Unit tests — no real network, no real Pali server

All unit tests run against an `httptest.Server` (Go) or `respx` mock (Python). Zero network I/O.

```go
// client_test.go pattern
func TestSearchMemory(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "/v1/memory/search", r.URL.Path)
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(SearchMemoryResponse{Items: []MemoryResponse{{ID: "m1"}}})
    }))
    defer srv.Close()

    c, _ := client.New(srv.URL)
    resp, err := c.SearchMemory(context.Background(), SearchMemoryRequest{TenantID: "t1", Query: "q"})
    require.NoError(t, err)
    assert.Len(t, resp.Items, 1)
}
```

### 8.2 Table-driven tests

Every function has a table-driven test covering: happy path, validation error, 4xx error, 5xx error, network timeout, context cancellation.

### 8.3 Coverage gate

`go test -cover ./pkg/...` must report **≥ 80% coverage** in CI.

### 8.4 Example tests (Go)

Every major operation has an `Example*` function in `example_test.go`. These are compiled and run by `go test` — they are live documentation.

```go
func ExampleClient_SearchMemory() {
    c, _ := client.New("http://localhost:8080", client.WithBearerToken("tok"))
    results, err := c.SearchMemory(context.Background(), client.SearchMemoryRequest{
        TenantID: "user:42",
        Query:    "coffee",
        TopK:     3,
    })
    if err != nil {
        log.Fatal(err)
    }
    for _, m := range results.Items {
        fmt.Println(m.Content)
    }
}
```

---

## 9. Documentation Requirements

### 9.1 Every exported symbol has a doc comment

```go
// StoreMemory persists a new memory for the given tenant.
// The returned StoreMemoryResponse contains the assigned memory ID and creation timestamp.
// Returns ErrUnauthorized if the bearer token is invalid.
func (c *Client) StoreMemory(ctx context.Context, req StoreMemoryRequest) (StoreMemoryResponse, error)
```

Python: every public function has a Google-style docstring.

```python
def store(self, tenant_id: str, content: str, **kwargs) -> StoreResponse:
    """Store a new memory for a tenant.

    Args:
        tenant_id: The tenant identifier (e.g. "user:42").
        content: The text content to remember.
        **kwargs: See StoreMemoryRequest for additional fields (tags, tier, kind).

    Returns:
        StoreResponse with the new memory's ID and created_at timestamp.

    Raises:
        PaliAuthError: If the token is invalid or expired.
        PaliNotFoundError: If the tenant does not exist.
        PaliError: For all other API errors.
    """
```

### 9.2 README structure (per-package)

Every SDK package's README must contain, in order:

1. **One-line description** — what it does
2. **Installation** — `go get` or `pip install`
3. **Quickstart** — working code, 5–10 lines, copy-pasteable
4. **Common patterns** — store, search, delete, middleware wrap
5. **Configuration reference** — table of all options + env vars
6. **Error handling** — how to check for specific errors
7. **Contributing** — link to root `CONTRIBUTING.md`

### 9.3 Changelog discipline

Every change that alters public types or method signatures requires a `docs/changes/YYYY-MM-DD-<slug>.md` entry with:
- What changed
- Why
- Migration path for existing callers

---

## 10. Versioning & Stability Guarantees

### 10.1 Stability tiers

| Tier | Marker | Promise |
|------|--------|---------|
| **Stable** | none | No breaking changes within a major version |
| **Beta** | `// BETA: may change` comment | Best-effort stability; breaking changes announced 30 days ahead |
| **Experimental** | `// EXPERIMENTAL` comment | Can break at any time; not for production use |

### 10.2 Semantic versioning

- `pkg/client` follows the Go module's version — all stable.
- `pali-client` (PyPI) follows semver independently.
- A breaking change in any stable symbol MUST bump the major version.

### 10.3 What counts as a breaking change

| Breaking | Not breaking |
|----------|-------------|
| Removing an exported function or type | Adding a new exported function |
| Changing a function signature | Adding a new field to a request struct |
| Removing a struct field | Adding a new field to a response struct |
| Changing error types | Changing log output |
| Changing default behavior without an option | New option with a safe default |

### 10.4 Deprecation process

1. Mark symbol with `// Deprecated: use X instead.` comment.
2. Keep it working for one full minor version.
3. Remove in the next major version.

---

## 11. Python-Specific Rules

### 11.1 Type annotations everywhere

All public functions, method arguments, and return values must be annotated. `py.typed` marker must be present so type checkers can consume the package.

```python
# pyproject.toml
[tool.mypy]
strict = true
```

### 11.2 Sync + async parity

Every method that makes a network call has both a sync and async version:

| Sync | Async |
|------|-------|
| `client.store(...)` | `await client.store_async(...)` |
| `client.search(...)` | `await client.search_async(...)` |

The async client is `PaliAsyncClient` — separate class, same interface, uses `httpx.AsyncClient` internally.

### 11.3 No hidden dependencies

`pali-client` core has one mandatory dependency: `httpx`. Everything else is optional:
- `pydantic` — install extra `pali-client[pydantic]`
- `openai` — install extra `pali-client[openai]` for middleware auto-wiring

```toml
[project.optional-dependencies]
pydantic = ["pydantic>=2.0"]
openai   = ["openai>=1.0"]
all      = ["pali-client[pydantic,openai]"]
```

### 11.4 Python version support

Support Python 3.10+ (matches `match` statement, `X | Y` union syntax).
Drop a version only when it reaches end-of-life (per python.org/downloads).

---

## 12. Go-Specific Rules

### 12.1 Interface for testability

The client exposes a `MemoryClient` interface in the same package so callers can mock it in their own tests:

```go
// Implemented by *Client; exposed so callers can mock.
type MemoryClient interface {
    StoreMemory(ctx context.Context, req StoreMemoryRequest) (StoreMemoryResponse, error)
    StoreMemoryBatch(ctx context.Context, req StoreMemoryBatchRequest) (StoreMemoryBatchResponse, error)
    SearchMemory(ctx context.Context, req SearchMemoryRequest) (SearchMemoryResponse, error)
    DeleteMemory(ctx context.Context, tenantID, memoryID string) error
}
```

> "Accept interfaces, return structs." — Go proverb.
> The `New()` constructor still returns `*Client` (concrete). The _interface_ is for callers to use as a parameter type.

### 12.2 No init() functions

`init()` functions that touch global state are forbidden. All initialization happens in `New()` / `NewClient()`.

### 12.3 go vet + staticcheck must pass

```makefile
lint-pkg:
    go vet ./pkg/...
    staticcheck ./pkg/...
    golangci-lint run ./pkg/...
```

### 12.4 Minimal allocations on the hot path

`SearchMemory` is the hot path. Response structs are returned by value (not pointer) when small, to minimize heap pressure. Avoid unnecessary `interface{}` boxing.

### 12.5 Zero external dependencies in pkg/client

`pkg/client` may only import the Go standard library. No third-party packages.
Rationale: callers should be able to vendor or use this without pulling in Pali's full dependency graph.

---

## 13. Middleware Pattern

The middleware is the highest-value developer experience feature. It wraps any LLM call and handles the memory lifecycle automatically.

### 13.1 The four-phase contract

```
[1: SEARCH]  Search relevant memories for the incoming message
      ↓
[2: INJECT]  Prepend memory context to the system prompt
      ↓
[3: CALL]    Invoke the underlying LLM (caller-provided function)
      ↓
[4: STORE]   Extract and store new facts from the LLM response
```

### 13.2 Go interface

```go
// pkg/middleware/middleware.go

// LLMFunc is any function that takes messages and returns a reply.
// It matches the shape of openai.Client.Chat, anthropic.Client.Messages, etc.
type LLMFunc func(ctx context.Context, messages []Message) (string, error)

type Middleware struct {
    memory  client.MemoryClient
    tenant  string
    opts    options
}

func New(memory client.MemoryClient, tenantID string, opts ...Option) *Middleware

// Wrap returns a new LLMFunc that executes the four phases around fn.
func (m *Middleware) Wrap(fn LLMFunc) LLMFunc
```

### 13.3 Python interface

```python
# pali/middleware.py

class PaliMiddleware:
    def __init__(self, client: PaliClient, tenant_id: str, **kwargs): ...

    def wrap(self, llm_fn: Callable) -> Callable:
        """Returns a wrapped function with memory inject + store lifecycle."""

    # OpenAI-compatible shortcut
    def wrap_openai(self, openai_client) -> "WrappedOpenAIClient":
        """Returns an OpenAI client drop-in with memory enabled."""
```

### 13.4 Options

```go
// How many memories to retrieve and inject
func WithTopK(k int) Option                   // default: 5

// min relevance score to inject (0.0–1.0)
func WithMinScore(s float64) Option           // default: 0.3

// Skip the store phase (read-only mode)
func WithReadOnly(v bool) Option              // default: false

// Custom system prompt template (must contain {{memories}} placeholder)
func WithSystemPromptTemplate(t string) Option

// Called after each phase; useful for logging/tracing
func WithHook(phase Phase, fn HookFunc) Option

// Allow delete/replace writeback actions during store phase.
// Default false: add-only writeback remains the safe default.
func WithDestructiveActions(v bool) Option

// Optional planner that can emit store / delete / replace actions
// after the LLM call using the original messages, recalled memories,
// and LLM response text.
func WithActionPlanner(fn ActionPlanner) Option
```

Python keyword equivalents:

```python
PaliMiddleware(
    client,
    tenant_id="user:42",
    allow_destructive_actions=False,
    action_planner=planner,
)
```

`allow_destructive_actions=False` is the default.
Middleware MUST default to add-only writeback. Delete and replace behavior are opt-in because they are materially riskier than adding new memory items.

When the backing API does not expose a first-class update endpoint yet, `replace` may be implemented as delete-plus-store behind the middleware.

### 13.5 Middleware must be transparent on error

If the search phase fails (network error, Pali down), the middleware **must still call the LLM** with an empty memory context — it must not fail the caller's LLM call. This is the "graceful degradation" contract.

Store-phase failures are **logged but never propagated** to the caller.

Planner failures or skipped destructive actions must never block the wrapped LLM call. They should surface only via hooks/logging.

---

## 14. Checklist (per-package)

Before any package is marked stable and shipped:

### Design
- [ ] Public surface reviewed against "minimal surface" principle
- [ ] All exported types documented with GoDoc / Google-style docstrings
- [ ] No global state mutations
- [ ] No `init()` side effects (Go)

### Correctness
- [ ] All happy paths covered by tests
- [ ] All error paths (4xx, 5xx, network timeout, context cancel) covered by tests
- [ ] Table-driven tests used
- [ ] Example tests compile and run (Go)

### Quality
- [ ] `go vet`, `staticcheck`, `golangci-lint` pass (Go)
- [ ] `mypy --strict` passes (Python)
- [ ] `pytest` with ≥80% coverage passes
- [ ] No panics reachable from public API (Go)

### Developer Experience
- [ ] README has quickstart that works copy-paste
- [ ] Configuration reference table exists
- [ ] Error handling section in README
- [ ] `PALI_BASE_URL` + `PALI_TOKEN` env vars recognized
- [ ] `MemoryClient` interface exported for mocking (Go)
- [ ] `py.typed` marker present (Python)
- [ ] Sync + async parity (Python async client)

### Packaging
- [ ] `pkg/client` has zero non-stdlib dependencies (Go)
- [ ] `pali-client` core depends only on `httpx` (Python)
- [ ] Optional extras declared in `pyproject.toml`
- [ ] Version tag matches semver

---

## References

These are the primary sources this guide draws from:

- [Google API Design Guide](https://cloud.google.com/apis/design) — resource-oriented naming, error model
- [Google Cloud Client Library Design](https://google.aip.dev/client-libraries) — AIP-4210, AIP-4221 (options pattern, retries)
- [Stripe Developer Experience Principles](https://stripe.com/blog/payment-api-design) — idempotency keys, typed errors, minimal surface
- [Go Package Naming](https://go.dev/blog/package-names) — short, lowercase, no stuttering
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) — error strings, receiver names, context first
- [Effective Go](https://go.dev/doc/effective_go) — interfaces, zero values, composition
- [Python Packaging User Guide](https://packaging.python.org) — pyproject.toml, extras, py.typed
- [httpx design](https://www.python-httpx.org/advanced/) — sync/async parity, transport abstraction
- [Twilio Helper Libraries Guide](https://www.twilio.com/docs/libraries) — consistent cross-language shape
