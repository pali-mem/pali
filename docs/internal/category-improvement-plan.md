# Category Improvement Rollout

This document tracks the additive rollout for improving:

- single-hop
- temporal
- open-domain

Multi-hop is intentionally out of scope here except for avoiding regressions.

## Implemented Changes

The following eval-side improvements are now present in [eval_locomo_f1_bleu.py](/C:/Users/sugam/Documents/pali/research/eval_locomo_f1_bleu.py):

- answer-type-aware routing and extractive heads
- early-rank reranking
- temporal answer shaping and relative-time handling
- deterministic open-domain alternative / label resolution before LLM fallback
- parser answer-span retention support
- profile support-link retrieval support
- random query sampling by default when `--max-queries` is smaller than the eval set

## New Flags

Server config:

```yaml
retrieval:
  answer_type_routing_enabled: false
  early_rank_rerank_enabled: false
  temporal_resolver_enabled: false
  open_domain_alternative_resolver_enabled: false

parser:
  answer_span_retention_enabled: false

profile_layer:
  support_links_enabled: false
```

Eval harness CLI:

- `--retrieval-answer-type-routing`
- `--retrieval-early-rank-rerank`
- `--retrieval-temporal-resolver`
- `--retrieval-open-domain-alternative-resolver`
- `--parser-answer-span-retention`
- `--profile-layer-support-links`
- `--query-sample-mode {random,head}`
- `--query-sample-seed <int>`

## What Changed In Behavior

- Single-hop:
  - prefers answer-shape extraction for quotes, booleans, lists, people/locations, frequencies, and direct attributes
  - tries to extract answer-bearing spans instead of generic descriptive clauses
- Temporal:
  - prefers event-bound temporal phrases
  - shapes answers to the question form, for example `year` or `month year`
- Open-domain:
  - uses more conservative deterministic resolution
  - requires focused evidence and clearer margins before committing to a label or binary answer
  - abstains more readily when evidence is weak or off-topic
- Sampling:
  - `--max-queries` now defaults to a random subset rather than the first `k`
  - reproducibility comes from `--query-sample-seed`

## Current Category-Focused Run Shape

Fresh-store or reuse-store category-improvement runs should include:

```powershell
--retrieval-answer-type-routing `
--retrieval-early-rank-rerank `
--retrieval-temporal-resolver `
--retrieval-open-domain-alternative-resolver `
--parser-answer-span-retention `
--profile-layer-support-links
```

Recommended eval sampling flags:

```powershell
--max-queries 5 `
--query-sample-mode random `
--query-sample-seed 1337
```

Use `--query-sample-mode head` only when you explicitly want the old first-`k` behavior.

## Latest Measured Scores

Baseline cached `mini5 q=150`:

- overall F1: `0.314150`
- overall BLEU-1: `0.255298`
- single-hop F1: `0.252338`
- temporal F1: `0.598348`
- open-domain F1: `0.180519`
- multi-hop F1: `0.139813`

Fresh-store cached `mini5 q=150` with the category-improvement slice enabled:

- overall F1: `0.325078`
- overall BLEU-1: `0.272265`
- single-hop F1: `0.284338`
- temporal F1: `0.596096`
- open-domain F1: `0.149495`
- multi-hop F1: `0.134129`

Interpretation:

- retrieval improved materially
- single-hop improved materially
- temporal is roughly flat
- open-domain is still below baseline
- multi-hop is slightly below baseline

## Metrics To Inspect

- `Recall@5`
- `Recall@10`
- `Recall@25`
- first-hit rank buckets
- single-hop F1 / BLEU
- temporal F1 / BLEU
- open-domain F1 / BLEU
- open-domain unknown-answer rate
- multi-hop regression tolerance

## Rollback

Rollback is config-only:

1. Set all new flags back to `false`.
2. Re-run the same cached eval.
3. Compare category deltas and early-rank recall deltas.

The SQLite schema change is additive (`metadata_json` on `memories`). No destructive migration is required.
