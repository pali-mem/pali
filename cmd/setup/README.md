# setup command

Prefer `pali init` for installed binaries. `go run ./cmd/setup` remains the source-checkout equivalent and prepares local project bootstrap state:

- creates required scaffold directories
- creates the target config file from `pali.yaml.example` when missing
- defaults to a zero-dependency lexical config so first boot does not require Ollama, ONNX, Qdrant, or Neo4j
- downloads missing ONNX assets when `embedding.provider=onnx` (or when forced with `-download-model`):
  - `models/all-MiniLM-L6-v2/model.onnx`
  - `models/all-MiniLM-L6-v2/tokenizer.json`
- checks ONNX Runtime shared library availability and prints OS-specific install hints when missing
- checks Ollama server/model readiness when the selected config enables an Ollama-backed embedder, parser, or scorer (`/api/version`, `/api/tags`)
  - uses `embedding.ollama_base_url` and `embedding.ollama_model` from the selected config file by default

It is safe to run multiple times.

Canonical config reference: `docs/configuration.md`.

Flags:

- `-config`: config file path to create/read during setup (default `pali.yaml`)
- `-skip-model-download`: do not download model files
- `-download-model`: force ONNX model download even if provider is not `onnx`
- `-skip-runtime-check`: skip ONNX Runtime shared library check
- `-skip-ollama-check`: skip Ollama readiness checks
- `-ollama-base-url`: override Ollama URL for checks (default from config)
- `-ollama-model`: override Ollama model for checks (default from config)
- `-model-id`: override Hugging Face model id (default `sentence-transformers/all-MiniLM-L6-v2`)
