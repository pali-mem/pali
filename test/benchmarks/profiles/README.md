# Benchmark Profiles

These wrappers are the checked-in benchmark entrypoints for release work. Each profile pins:

- fixture path
- eval set path
- backend
- embedding provider/model
- top-k and query selection mode

Use them when you want a named, reproducible run instead of rebuilding the command by hand.

Profiles:

- `release-curated-ollama.sh`: primary retrieval-quality gate on the checked-in curated fixture/eval set.
- `release-curated-lexical.sh`: local and CI smoke profile using the same data with lexical embeddings.
- `release-curated-openrouter.sh`: retrieval-quality profile using OpenRouter embeddings (`OPENROUTER_API_KEY` required).
- `throughput-ollama.sh`: store and search throughput profile on the same canonical fixture.
- `throughput-lexical.sh`: local/CI throughput profile without external model dependencies.
- `suite-local.sh`: config-driven modular suite (sqlite+lexical speed + retrieval).
- `suite-medium-fast.sh`: medium-size fast suite (500 fixture rows / 231 eval queries).
- `suite-medium-qdrant-openrouter.sh`: medium-size qdrant + OpenRouter suite (`OPENROUTER_API_KEY` required; remote embedder comparison lane).
- `suite-medium-qdrant-openrouter-parser-graph.sh`: medium-size qdrant + OpenRouter suite with OpenRouter parser and graph singleton invalidation enabled.
- `suite-qdrant-ollama.sh`: config-driven profile-paired suite (lexical baseline vs qdrant+ollama, includes scorer comparisons).
- `suite-locomo-optional.sh`: optional LoCoMo suite template (disabled scenario by default; enable in suite JSON first). This lane defaults to `retrieval_kind_routing=true`, `retrieval_answer_type_routing=true`, `retrieval_early_rank_rerank=true`, and `retrieval_temporal_resolver=true`.

Each run writes the rendered config and source provider profile into the result folder:

- `config.profile.yaml`
- `config.rendered.yaml`

That makes the result folder self-describing and lets others replay the exact runtime configuration used.
