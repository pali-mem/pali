# setup command

`go run ./cmd/setup` prepares local project bootstrap state:

- creates required scaffold directories
- creates `pali.yaml` from `pali.yaml.example` when missing
- downloads missing ONNX assets when `embedding.provider=onnx` (or when forced with `-download-model`):
  - `models/all-MiniLM-L6-v2/model.onnx`
  - `models/all-MiniLM-L6-v2/tokenizer.json`
- checks ONNX Runtime shared library availability and prints OS-specific install hints when missing
- checks Ollama server/model readiness for offline embedding defaults (`/api/version`, `/api/tags`)
  - defaults to `embedding.ollama_base_url` and `embedding.ollama_model` from `pali.yaml`

It is safe to run multiple times.

Canonical config reference: `docs/configuration.md`.

Flags:

- `-skip-model-download`: do not download model files
- `-download-model`: force ONNX model download even if provider is not `onnx`
- `-skip-runtime-check`: skip ONNX Runtime shared library check
- `-skip-ollama-check`: skip Ollama readiness checks
- `-ollama-base-url`: override Ollama URL for checks (default from `pali.yaml`)
- `-ollama-model`: override Ollama model for checks (default from `pali.yaml`)
- `-model-id`: override Hugging Face model id (default `sentence-transformers/all-MiniLM-L6-v2`)
