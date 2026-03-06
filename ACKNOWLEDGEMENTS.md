# Acknowledgements

Pali builds on a number of research works, open-source libraries, and models.

---

## Research

### Weighted Memory Retrieval (WMR)

Pali's retrieval scoring is synthesized from two foundational papers:

**Park et al. (2023) — Generative Agents: Interactive Simulacra of Human Behavior**
> Introduced the three-factor scoring framework of **recency × importance × relevance**, normalized via min-max scaling and combined as a weighted sum.
> [https://arxiv.org/abs/2304.03442](https://arxiv.org/abs/2304.03442)

**Zhong et al. (2024) — MemoryBank: Enhancing Large Language Models with Long-Term Memory (AAAI 2024)**
> Introduced the **Ebbinghaus Forgetting Curve**-based decay mechanism applied to memory recency (e.g. `decay_factor ^ hours_since_last_access`).
> [https://arxiv.org/abs/2305.10250](https://arxiv.org/abs/2305.10250)

### Ebbinghaus Forgetting Curve

**Hermann Ebbinghaus (1885) — Über das Gedächtnis (Memory: A Contribution to Experimental Psychology)**
> The original psychological research on exponential memory decay over time, which underpins the recency scoring component used by MemoryBank and adopted in Pali's WMR implementation.

---

## Models

**`all-MiniLM-L6-v2`** — sentence-transformers / Microsoft
> The default embedding model used for semantic similarity search.
> Distributed under Apache 2.0.
> [https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2](https://huggingface.co/sentence-transformers/all-MiniLM-L6-v2)

---

## Core Go Dependencies

| Package | License | Use |
|---|---|---|
| [`gin-gonic/gin`](https://github.com/gin-gonic/gin) | MIT | HTTP router and middleware |
| [`modernc.org/sqlite`](https://gitlab.com/cznic/sqlite) | BSD/MIT | Pure-Go CGo-free SQLite driver |
| [`google/uuid`](https://github.com/google/uuid) | BSD-3-Clause | Memory and tenant ID generation |
| [`stretchr/testify`](https://github.com/stretchr/testify) | MIT | Test assertions |
| [`gopkg.in/yaml.v3`](https://github.com/go-yaml/yaml) | MIT/Apache-2.0 | Config file parsing |

## Indirect / Transitive Dependencies (notable)

| Package | License | Use |
|---|---|---|
| [`bytedance/sonic`](https://github.com/bytedance/sonic) | Apache-2.0 | Fast JSON serialization (via gin) |
| [`goccy/go-json`](https://github.com/goccy/go-json) | MIT | JSON fallback (via gin) |
| [`go-playground/validator`](https://github.com/go-playground/validator) | MIT | Request validation (via gin) |
| [`google/go-cmp`](https://github.com/google/go-cmp) | BSD-3-Clause | Deep equality in tests |
| [`ncruces/go-strftime`](https://github.com/ncruces/go-strftime) | MIT | SQLite strftime support |
| [`remyoudompheng/bigfft`](https://github.com/remyoudompheng/bigfft) | BSD-3-Clause | Arbitrary precision math (modernc.org/sqlite) |

---

## Planned / Optional Dependencies

These are not yet in `go.mod` but are referenced in the architecture:

| Package | License | Use |
|---|---|---|
| [`cohesion-org/deepseek-go`](https://github.com/cohesion-org/deepseek-go) | MIT | Ollama-compatible Go client for LLM-based importance scoring (opt-in) |
| [ONNX Runtime](https://github.com/microsoft/onnxruntime) | MIT | Native library required for local ONNX embedding inference |
| [sqlite-vec](https://github.com/asg017/sqlite-vec) | MIT/Apache-2.0 | Embedded vector search extension for SQLite (default backend) |
| [Qdrant](https://github.com/qdrant/qdrant) | Apache-2.0 | External vector database (opt-in scale backend) |
| [pgvector](https://github.com/pgvector/pgvector) | PostgreSQL License | Postgres vector extension (opt-in scale backend) |
| [HTMX](https://htmx.org) | BSD-2-Clause | Dashboard frontend interactivity (zero-JS build) |

---

## Go Standard Library

Pali makes extensive use of the Go standard library, distributed under the [Go BSD-style license](https://go.dev/LICENSE).
