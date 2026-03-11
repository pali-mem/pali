# 2026-03-08 LOCOMO OpenRouter + Qdrant Progress

## Why

Record the current best 150-query LOCOMO paper-lite run after moving the eval profile to:

- `qdrant` vector backend
- `openrouter` embeddings
- `openrouter` importance scoring
- `openrouter` parser
- `openrouter` answer generation

This note also captures the concrete fixes that moved the score from the low-teens into the mid-20s.

## Final recorded run

Artifacts:

- JSON: `research/results/openrouter_qdrant/20260308T154935-reuse-final150-multihop-openfix2/locomo150.multihop.openfix2.json`
- Summary: `research/results/openrouter_qdrant/20260308T154935-reuse-final150-multihop-openfix2/locomo150.multihop.openfix2.summary.txt`

Run profile:

- `max_queries=150`
- `top_k=60`
- `eval_workers=50`
- vector backend: `qdrant`
- embedding provider/model: `openrouter / sentence-transformers/all-minilm-l12-v2:nitro`
- importance scorer/model: `openrouter / openai/gpt-oss-120b:nitro`
- answer mode/provider/model: `generate / openrouter / openai/gpt-oss-120b:nitro`
- parser provider/model: `openrouter / openai/gpt-5-mini:nitro`
- multi-hop decomposition provider/model: `openrouter / openai/gpt-oss-120b:nitro`
- answer top docs: `8`
- retrieval query variants: `4`
- kind routing: `on`
- multi-hop decomposition: `on`
- entity fact backend: `sqlite`
- temporal raw-turn routing: `on`
- structured memory: `off`
- parser raw-turn storage: `on`
- store mode: `reuse_existing_store`

## Final metrics

Overall:

| Metric | Value |
|---|---:|
| F1 generated | `0.287032` (`28.70`) |
| BLEU-1 generated | `0.231493` (`23.15`) |
| Final score | `25.93` |
| EM generated | `0.066667` |
| F1 no-stopwords | `0.287037` |

Retrieval:

| Metric | Value |
|---|---:|
| Recall@60 | `0.567778` |
| nDCG@60 | `0.268176` |
| MRR | `0.198060` |
| Top1 unique rate | `0.500000` |

Category breakdown:

| Category | Count | F1 | BLEU-1 |
|---|---:|---:|---:|
| Multi-hop | `31` | `14.19` | `9.37` |
| Temporal | `37` | `39.04` | `31.50` |
| Open-domain | `11` | `28.36` | `17.82` |
| Single-hop | `70` | `30.13` | `26.01` |
| Adversarial | `1` | `0.00` | `0.00` |

Answer path distribution:

- `extractive_fallback_generate`: `56`
- `generator_only`: `91`
- `open_domain_unknown_generate`: `3`

## What moved the score

The score improvement was not mainly from swapping providers. The largest gains came from answer-path and evidence-path fixes:

1. Added OpenRouter answer generation support to the eval harness.
2. Added OpenRouter parser support to the eval harness.
3. Fixed `generate` mode so it falls back to extractive when generation fails or returns `Unknown`.
4. Added low-signal evidence filtering to remove short conversational acknowledgements from the answer context.
5. Increased temporal evidence weighting and penalized non-temporal lines for temporal questions.
6. Raised eval HTTP concurrency to `50`.
7. Switched answer-time context selection from a fixed top-doc slice to a wider query-scored context chooser.
8. Added multi-candidate extractive answer generation and fed those candidates into the answer prompt.
9. Restricted free-form inference prompting to `why` / `how` questions, which improved the overall score after the broader answer-stack change.
10. Wired `retrieval.multi_hop` and entity-fact backend settings into the LOCOMO eval server config, so the new server-side multi-hop path is actually exercised during eval.
11. Added generated-answer snapping and spacing repair so OpenRouter outputs like `EdSheeran`, `Likelyyes`, and `familyand` no longer lose F1 to formatting alone.
12. Added an open-domain resolver path with stricter extractive fallback, focus-aware evidence scoring, and heuristic label synthesis for likely / counterfactual / trait / leaning questions.

## Important intermediate deltas

Key same-profile milestones:

| Run | Final | F1 | BLEU-1 |
|---|---:|---:|---:|
| OpenRouter parser, before fallback fix | `12.98` | `14.26` | `11.69` |
| After generate->extractive fallback fix | `19.72` | `21.69` | `17.76` |
| After evidence cleanup | `20.79` | `23.09` | `18.49` |
| After wider answer context + candidate answer stack | `24.85` | `27.45` | `22.25` |
| After guarded inference prompt | `25.19` | `27.97` | `22.41` |
| After multi-hop eval wiring + answer snapping + open-domain resolver | `25.93` | `28.70` | `23.15` |

Interpretation:

- The fallback fix was the big unlock.
- Evidence cleanup gave a smaller but real gain on top.
- Widening answer-time context and adding candidate answer reranking was the second major jump.

## What the trace showed

From the traced 150-query OpenRouter parser run before the fallback fix:

- `unknown_rate = 58.67%`
- `low_f1_rate (<=0.05) = 74.00%`
- `hit_rate = 63.33%`
- `hit_but_lowf1_rate = 42.67%`

Root cause summary:

- OpenRouter transport failures were real but small (`8/150` in the traced run).
- The bigger issue was model abstention plus answer-path handling:
  - `88/150` generated answers were `Unknown`
  - every one of those `88` cases still had a non-unknown extractive candidate
  - many relevant hits were outside the top answer-context window
  - evidence selection was still admitting noisy conversational lines

## Current bottlenecks

The remaining ceiling is mostly in answer quality, not raw retrieval:

1. `qa_metrics.f1_oracle_sentence_topk = 0.353867`
2. `qa_metrics.f1_generated = 0.279740`
3. Oracle gap is still `~0.074`

That means the system often retrieves enough signal somewhere in top-k, but the final answer path still fails to turn it into the best short answer.

Most likely next wins:

1. Better answer-context selection so relevant hits do not fall outside `answer_top_docs=8`.
2. Better extractive ranking/compression for open-domain and single-hop answers.
3. Multi-hop-specific retrieval and composition work, likely involving graph/entity edges later.
4. Structured memory and structured query routing experiments once the current answer layer is more stable.

## Notes

- This run reused the existing store and index map with `--override-fingerprint` because parser provider/model changes alter the config fingerprint.
- `structured_memory` remained off in this recorded run, so these gains came without graph-assisted routing yet.
- The score gain in the latest step came mainly from answer-path fixes and open-domain handling, not from a major retrieval jump.

## Clean profile-layer update

After removing the LOCOMO-shaped open-domain fallback heuristics, the next real gain came from adding a clean profile layer on top of the same OpenRouter + Qdrant stack.

Artifacts:

- JSON: `research/results/openrouter_qdrant/20260308T165247-reuse-final150-profile-layer-gptoss-isolated/locomo150.profile_layer.gptoss.isolated.json`
- Summary: `research/results/openrouter_qdrant/20260308T165247-reuse-final150-profile-layer-gptoss-isolated/locomo150.profile_layer.gptoss.isolated.summary.txt`

Run profile additions:

- profile layer: `on`
- profile provider/model: `openrouter / openai/gpt-oss-120b:nitro`
- profile summaries stored: `20`
- open-domain profile retrieval route: `on`
- open-domain query rewrite: `off`

Profile-layer design:

1. Build per-entity profile summaries from each tenant's conversation history.
2. Store those summaries as regular `summary` memories with `profile` tags.
3. Route open-domain queries to an additional `summary`-only retrieval pass.
4. Keep profile summaries out of the default multi-hop base route so they do not drown out event/raw-turn evidence.

Clean delta vs the previous clean OpenRouter baseline (`20260308T160826-reuse-final150-clean-opendomain-gptoss`):

| Metric | Baseline | Profile layer | Delta |
|---|---:|---:|---:|
| F1 generated | `28.45` | `30.00` | `+1.55` |
| BLEU-1 generated | `23.25` | `25.07` | `+1.82` |
| Final score | `25.85` | `27.54` | `+1.69` |
| Recall@60 | `0.5678` | `0.5967` | `+0.0289` |

Category delta:

| Category | Baseline F1 | Profile-layer F1 | Delta |
|---|---:|---:|---:|
| Multi-hop | `14.92` | `17.76` | `+2.84` |
| Temporal | `39.04` | `39.94` | `+0.90` |
| Open-domain | `10.68` | `14.52` | `+3.84` |
| Single-hop | `32.05` | `33.03` | `+0.98` |

Interpretation:

- This is the first clean architectural change that improved all non-adversarial categories at once.
- Open-domain remains the weakest category, but the profile layer materially closes the gap without benchmark-specific heuristics.
- The main remaining problem is not whether profile memories help. They do.
- The remaining problem is profile answer quality and profile retrieval precision. We need stronger profile content and better answer grounding on top of the profile layer.

## Fresh facet retrieval with stable answer generation

The earlier fresh facet run showed a large temporal jump and strong retrieval, but it also suffered from high generation failures. After adding app-level answer retries, switching the answer hot path to Gemini Flash, and fixing eval route handling so category-specific vector passes are not skipped when `structured_memory=false`, the same general facet profile became much more balanced.

Artifacts:

- JSON: `research/results/openrouter_qdrant/20260308T195940-fresh150-facets-gptoss-gemini/locomo150.facets.gptoss.gemini.json`
- Summary: `research/results/openrouter_qdrant/20260308T195940-fresh150-facets-gptoss-gemini/locomo150.facets.gptoss.gemini.summary.txt`

Run profile:

- fresh store: `true`
- vector backend: `qdrant`
- embed / score / parser / multihop / profile model: `openrouter / openai/gpt-oss-120b:nitro`
- answer model: `openrouter / google/gemini-2.0-flash-001`
- profile layer: `facets`
- profile stored: `106 facets / 18 entities`
- eval workers: `50`
- store batch size: `32`

Delta vs the earlier fresh facet run (`20260308T173713-fresh-final150-profile-facets-gptoss`):

| Metric | Old facets | New facets + Gemini | Delta |
|---|---:|---:|---:|
| F1 generated | `31.41` | `34.02` | `+2.61` |
| BLEU-1 generated | `25.97` | `28.97` | `+3.00` |
| Final score | `28.69` | `31.50` | `+2.81` |
| Gen failures | `22` | `3` | `-19` |
| Recall@60 | `0.7772` | `0.7739` | `-0.0033` |

Category delta:

| Category | Old facets F1 | New facets + Gemini F1 | Delta |
|---|---:|---:|---:|
| Multi-hop | `15.22` | `14.83` | `-0.39` |
| Temporal | `62.40` | `59.87` | `-2.53` |
| Open-domain | `6.44` | `19.55` | `+13.11` |
| Single-hop | `25.14` | `30.19` | `+5.05` |

Interpretation:

- The extreme temporal score was partly real retrieval strength, but it was paired with an unstable answer path.
- Once answer reliability improved, temporal stayed very strong while open-domain and single-hop recovered sharply.
- This is a better overall operating point than the earlier facet run because it keeps most of the retrieval gain without sacrificing the other categories as badly.
- Multi-hop still lags and remains the next retrieval-side problem. Open-domain improved materially, but it is still well below the publication target range.
