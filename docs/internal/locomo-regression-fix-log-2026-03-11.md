# LOCOMO Regression Fix Log (2026-03-11)

## Scope

Implemented the first-pass regression fixes for the clean `mini5` LOCOMO benchmark path, then reran four fresh-store clean benchmarks on the same random sample (`max_queries=60`, `seed=1337`).

## Code changes

### Eval workflow / guardrails

- Extended config fingerprinting in `research/eval_locomo_f1_bleu.py` to hash store-shaping code, not just CLI flags.
- Added fresh-store audit checks in `research/eval_locomo_f1_bleu.py` for:
  - generic query-view rate
  - parser scaffold noise rate
  - answer metadata coverage when `--parser-answer-span-retention` is enabled
- Added non-temporal time-only answer rejection and open-domain answer-type routing fixes in `research/eval_locomo_f1_bleu.py`.
- Added single-hop extractive preference and candidate snapping so generation does not drift away from strong grounded candidates.
- Tightened generation prompt in `research/prompts.py` so the model must prefer one candidate answer instead of synthesizing a new mixed phrase.

### Memory/store fixes

- `internal/core/memory/fact_quality.go`
  - reject parser scaffold facts
  - reject time-only speech scaffolds
  - remove generic `what about X` / `what did X do` query-view rewrites
- `internal/core/memory/store.go`
  - filter query-view text before embedding/storage
- `internal/repository/sqlite/memory.go`
  - stop duplicating query-view text in FTS indexed text
- `internal/core/memory/postprocess_worker.go`
  - persist `fact.AnswerMetadata` in async parser-derived memory writes
- `internal/core/memory/store_batch_parser.go`
  - persist `fact.AnswerMetadata` in batch parser staging path

### Regressions added

- `internal/core/memory/postprocess_worker_test.go`
  - async parser worker retains metadata and drops scaffold facts
- `internal/core/memory/store_parser_test.go`
  - batch parser path retains answer metadata
- `internal/core/memory/store_parser_test.go`
  - prep rejects parser scaffold timestamp facts
  - prep drops generic query-view lines
- `research/test_eval_locomo_f1_bleu.py`
  - open-domain grounded clause extraction
  - scaffold sentence downweighting
  - single-hop route preference
  - high-confidence extractive preference

## Root cause fixed during this run

The fresh-store benchmark initially failed because answer-span metadata was still missing in two internal write paths:

1. `internal/core/memory/postprocess_worker.go`
   - async parser postprocess stored parsed facts without `AnswerMetadata`
2. `internal/core/memory/store_batch_parser.go`
   - batch parser staging path also dropped `AnswerMetadata`

That is why the clean run previously showed `blank_metadata=100%` even after enabling parser answer-span retention.

## Verification

Commands run locally:

```powershell
go test ./internal/core/memory ./internal/repository/sqlite
python -m unittest research.test_eval_locomo_f1_bleu
```

Both passed after the fixes.

## Clean benchmark commands

```powershell
powershell -ExecutionPolicy Bypass -File research/run_fastsmoke_locomo_mini_clean.ps1 -Label control-clean-a -MaxQueries 60 -Port 18126 -QuerySeed 1337
powershell -ExecutionPolicy Bypass -File research/run_fastsmoke_locomo_mini_clean.ps1 -Label full-fixed-b -MaxQueries 60 -Port 18127 -QuerySeed 1337 -AnswerTypeRouting -EarlyRerank -TemporalResolver -OpenDomainAlternativeResolver -ParserAnswerSpanRetention -ProfileLayerSupportLinks
powershell -ExecutionPolicy Bypass -File research/run_fastsmoke_locomo_mini_clean.ps1 -Label full-fixed-no-openalt-a -MaxQueries 60 -Port 18128 -QuerySeed 1337 -AnswerTypeRouting -EarlyRerank -TemporalResolver -ParserAnswerSpanRetention -ProfileLayerSupportLinks
powershell -ExecutionPolicy Bypass -File research/run_fastsmoke_locomo_mini_clean.ps1 -Label full-fixed-no-earlyrerank-a -MaxQueries 60 -Port 18129 -QuerySeed 1337 -AnswerTypeRouting -TemporalResolver -OpenDomainAlternativeResolver -ParserAnswerSpanRetention -ProfileLayerSupportLinks
```

## Results

| Run | F1 | BLEU-1 | Recall@60 | nDCG@60 | MRR | Single-hop F1 | Temporal F1 | Open-domain F1 | Multi-hop F1 | Blank metadata |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| control-clean-a | 0.1977 | 0.1526 | 0.7857 | 0.3947 | 0.3078 | 0.1921 | 0.5370 | 0.0571 | 0.0309 | 100.0% |
| full-fixed-b | 0.2067 | 0.1627 | 0.7996 | 0.5190 | 0.4513 | 0.2156 | 0.5370 | 0.0000 | 0.0348 | 33.9% |
| full-fixed-no-openalt-a | 0.2115 | 0.1675 | 0.7996 | 0.4916 | 0.4141 | 0.2251 | 0.5123 | 0.0000 | 0.0501 | 33.8% |
| full-fixed-no-earlyrerank-a | 0.2114 | 0.1667 | 0.7829 | 0.4883 | 0.4314 | 0.2262 | 0.5370 | 0.0000 | 0.0296 | 33.8% |

## Readout

- The implemented fixes improved clean-run overall score versus control.
- Retrieval improved materially in every fixed run.
- Single-hop improved materially in every fixed run.
- Temporal stayed flat or nearly flat.
- Open-domain remains the main unresolved problem on this sample. All fixed runs collapsed to `0.0000` open-domain F1.
- The best clean run in this batch was `full-fixed-no-openalt-a`.

## What the ablations imply

- `Open-domain alt` is still not helping this seed. Turning it off produced the best overall score in this batch.
- `Early rerank` is not the main source of the remaining answer-selection issue. Removing it kept overall score near the best run, but reduced retrieval quality.
- The metadata fixes are real. Fresh-store `blank_metadata` fell from `100%` to about `33.8%`.

## Output locations

- `research/results/neo4j_locomo/20260311T025009Z-mini-clean-control-clean-a/locomo.fastsmoke.summary.txt`
- `research/results/neo4j_locomo/20260311T032557Z-mini-clean-full-fixed-b/locomo.fastsmoke.summary.txt`
- `research/results/neo4j_locomo/20260311T034252Z-mini-clean-full-fixed-no-openalt-a/locomo.fastsmoke.summary.txt`
- `research/results/neo4j_locomo/20260311T035936Z-mini-clean-full-fixed-no-earlyrerank-a/locomo.fastsmoke.summary.txt`

## Next exact target

The next fix should be open-domain resolution only. The clean data now says:

- retrieval is no longer the bottleneck
- single-hop got better
- metadata storage now works
- open-domain answer resolution still over-abstains or over-normalizes

That next pass should be trace-driven against the five open-domain items from the `seed=1337` sample.
