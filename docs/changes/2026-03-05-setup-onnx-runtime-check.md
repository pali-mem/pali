# 2026-03-05: Setup now checks ONNX Runtime shared library

## Summary

Updated `cmd/setup` so it validates ONNX Runtime shared library availability during setup and prints OS-specific install hints when missing.

Changes in `cmd/setup/main.go`:

- Added ONNX Runtime check after model/config bootstrap.
- Added `-skip-runtime-check` flag for offline/minimal bootstrap.
- Added platform-specific hints:
  - macOS: `brew install onnxruntime`
  - Windows: use ONNX Runtime release zip + install Visual C++ runtime
  - Linux/other: use release archive + set `ONNXRUNTIME_SHARED_LIBRARY_PATH`

## Why

Previously, setup only downloaded model files. Users could still fail later at runtime with:

- `dlopen(libonnxruntime.dylib): no such file`

This change surfaces the missing dependency earlier and provides explicit next steps.

## Notes

- This is a detection/guidance improvement; setup does not auto-install system libraries.
- ONNX remains optional unless `embedding.provider=onnx` is selected.
