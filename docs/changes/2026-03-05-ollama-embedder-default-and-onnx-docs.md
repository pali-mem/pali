# 2026-03-05: Ollama embedder provider + default embedding path

## Summary

Shifted embedding UX toward offline-first simplicity:

- Added real `internal/embeddings/ollama` provider using Ollama HTTP API.
- Added startup preflight checks for Ollama provider:
  - `GET /api/version`
  - `GET /api/tags`
  - configured model must exist (else actionable `ollama pull` message)
- Added shared embedding factory (`internal/embeddings/factory.go`) used by both API and MCP binaries.
- Extended config embedding fields:
  - `ollama_base_url`
  - `ollama_model`
  - `ollama_timeout_seconds`
- Changed default `embedding.provider` to `ollama` with default model `all-minilm`.
- Kept ONNX as advanced opt-in path and added dedicated docs at `docs/onnx.md` (macOS/Windows/Linux guidance).
- Clarified that `mock` provider is local-development only.

## Why

Users faced friction with ONNX shared-library setup as a default path. Ollama provider gives a much simpler offline experience while retaining ONNX for advanced users.

## Validation

- Added Ollama embedder unit tests:
  - provider preflight success + embed call
  - missing model detection
  - unreachable server error hints
- Added config validation tests for ollama/onnx requirements.
- Updated API/integration tests to pin `embedding.provider=mock` where deterministic local test behavior is required.
