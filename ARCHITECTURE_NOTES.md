# Architecture Notes: Benchmark Tuning vs. Core Product

## Issue: M2 Temporal Routing (Identified 2026-03-05)

### Current State (Problem)
**M2 (Temporal raw_turn routing) is implemented only in the eval harness**, not in pali core:

- File: `research/eval_locomo_f1_bleu.py` → `build_retrieval_routes()`
- Logic: Python script detects temporal queries via regex and sends `kinds=["event", "observation", "raw_turn"]` to pali
- Impact: Only benefits benchmark runs, not real API users
- This is **test-harness tuning, not a product feature**

### The Problem
If a user calls pali's `/v1/memory/search` API with a temporal question and doesn't explicitly specify kinds, they won't get the temporal routing benefit. The server has no built-in logic to:
1. Detect query intent (temporal/person/multi-hop)
2. Auto-route to appropriate memory kinds
3. Apply query-specific boosts

### Why It Matters
At the end of all M1–M5 milestones, we want to answer: **"What real product improvements did we ship?"**

| Feature | Location | Shipping? | Notes |
|---------|----------|-----------|-------|
| M1: Timestamp injection | Go code ✅ | YES | Real feature in `/v1/memory` |
| M2: Temporal routing (current) | Python harness ❌ | NO | Only helps in benchmark |
| M2: Temporal routing (should be) | Go code | YES | Auto-routing in search handler |
| M3: Two-pass retrieval (MVP) | Python harness ❌ | NO | Benchmark-only for now, decide after results |
| M3: Two-pass retrieval (future) | Go code | MAYBE | Only if latency tradeoff justified |
| M4: Embedding model swap | Config ✅ | YES | Already pluggable |
| M5: Fact-length floor | Go code ✅ | YES | Already in `store.go` |

### Required Changes (Before shipping)

**Move M2 into pali core:**

1. **Add query classification to Go** (`internal/core/memory/query_routing.go`):
   ```go
   func classifyQuery(query string) QueryProfile {
       // Detect temporal, person, multi-hop intent
       // Return routing recommendation
   }
   ```

2. **Auto-route in search handler** (`internal/api/handlers/memory.go`):
   ```go
   func (h *MemoryHandler) Search(w http.ResponseWriter, r *http.Request) {
       // ...
       if len(req.Kinds) == 0 && h.config.QueryRoutingEnabled {
           req.Kinds = h.suggestedKinds(req.Query) // Apply auto-routing
       }
   }
   ```

3. **Update eval harness** to NOT do kind-routing itself:
   - Remove `build_retrieval_routes()` logic from Python
   - Let pali server handle it (via empty Kinds field)
   - Validate that server's routing matches expected behavior

### Timeline
- **Now**: Continue with M1+M2 validation in harness (for benchmark feedback)
- **Before shipping**: Refactor M2 into Go handler
- **Testing**: Verify eval runs still pass with server-side routing

### Implications
- **Without refactor**: Benchmark gets fixed, but nobody using the API benefits
- **With refactor**: Real users get better temporal query handling out-of-the-box
- **Scope**: Medium — ~200 lines of Go + tests, simplifies eval harness

### Decision Log
- 2026-03-05: Identified that M2 is eval-harness-only; flagged for redesign

---

## Other Architectural Debt (Future Consideration)

### M3: Two-pass retrieval
Currently designed as eval-harness post-processing. Should this also move to `search.go`?
- Pro: Real product feature, available to all API users
- Con: Adds latency (2 queries instead of 1), needs configurable threshold
- Decision: Defer until after M2 refactor

### Entity-facts table (P2)
Needs SQLite migration + new CRUD repository. Design is sound; implementation straightforward.
- Timeline: After P1 milestones validated
