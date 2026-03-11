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

Each run writes the rendered config and source provider profile into the result folder:

- `config.profile.yaml`
- `config.rendered.yaml`

That makes the result folder self-describing and lets others replay the exact runtime configuration used.
