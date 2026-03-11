# LOCOMO Status Note (2026-03-11)

This note records the current interpretation of the March 11 LOCOMO regression work so benchmark numbers are not over-read.

## What improved

- The latest eval-layer patch materially improved the sampled LOCOMO scores on the existing clean store.
- The recovery came from answer selection and answer finalization changes in the eval harness, not from rebuilding memories or changing retrieval storage.
- In practical terms, the benchmark drop was not solely a "Pali core retrieval is bad now" signal.

## What the quick reuse run means

- The quick reuse run is a useful A/B check for eval logic on fixed data.
- It shows that a meaningful part of the recent drop was downstream of retrieval.
- It does **not** by itself establish the final benchmark number for the full current worktree.

## Why the new rewrites are not in place yet

- The more aggressive store-side and rewrite-side fixes are still separate work:
  - query-view generation cleanup
  - scaffold/noise rejection at ingest
  - stricter fact admission
  - metadata coverage improvements on more memory kinds
- Those changes alter store shape and therefore need a fresh benchmark path, not a quick reuse-only check.
- The current eval patch was intentionally kept narrow so we could recover benchmark quality quickly without conflating it with storage changes.

## Current benchmark stance

- Retrieval on the clean store is still in a workable range.
- Answer selection and abstention policy were the larger recent regression factors.
- The quick reuse result should be read as: "there is recoverable score left in the current pipeline," not "all benchmark issues are solved."

## Practical implication

- The eval-layer recovery can be shipped now.
- The store/rewrite cleanup should land as a follow-up and be measured with a strict clean-store benchmark once those code paths are finalized.
