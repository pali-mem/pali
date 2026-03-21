# ONNX Runtime Setup (Advanced)

Use this path when you want `embedding.provider: onnx`.

Pali's ONNX provider uses [`github.com/yalue/onnxruntime_go`](https://github.com/yalue/onnxruntime_go), which requires a platform-native ONNX Runtime shared library at runtime.

## Requirements

1. ONNX model files:
   - `models/all-MiniLM-L6-v2/model.onnx`
   - `models/all-MiniLM-L6-v2/tokenizer.json`
2. ONNX Runtime shared library (`.dylib`, `.so`, or `.dll`)

## macOS

1. Install ONNX Runtime:
   - `brew install onnxruntime`
   - Formula reference: <https://formulae.brew.sh/formula/onnxruntime>
2. Verify library path:
   - usually `/opt/homebrew/lib/libonnxruntime.dylib` on Apple Silicon
3. If needed, set:
   - `export ONNXRUNTIME_SHARED_LIBRARY_PATH=/opt/homebrew/lib/libonnxruntime.dylib`

## Windows

1. Download ONNX Runtime release package (C/C++ binaries):
   - <https://github.com/microsoft/onnxruntime/releases>
2. Extract and locate `onnxruntime.dll`.
3. Set environment variable:
   - `setx ONNXRUNTIME_SHARED_LIBRARY_PATH "C:\\path\\to\\onnxruntime.dll"`
4. Install Microsoft Visual C++ 2019 runtime if missing:
   - requirement noted by `onnxruntime_go`: <https://github.com/yalue/onnxruntime_go/blob/master/README.md>

## Linux

1. Download ONNX Runtime package from releases:
   - <https://github.com/microsoft/onnxruntime/releases>
2. Locate `libonnxruntime.so` and set:
   - `export ONNXRUNTIME_SHARED_LIBRARY_PATH=/path/to/libonnxruntime.so`

## Config Example

```yaml
embedding:
  provider: onnx
  model_path: ./models/all-MiniLM-L6-v2/model.onnx
  tokenizer_path: ./models/all-MiniLM-L6-v2/tokenizer.json
```

## Troubleshooting

- Error: `Error loading ONNX shared library ...`
  - shared library path is missing/wrong
  - set `ONNXRUNTIME_SHARED_LIBRARY_PATH` explicitly
- Error: model file not found
  - run `pali init -download-model` to fetch missing model artifacts

## References

- ONNX Runtime install docs: <https://onnxruntime.ai/docs/install/>
- ONNX Runtime C/C++ package notes: <https://onnxruntime.ai/docs/get-started/with-c.html>
- `onnxruntime_go` README: <https://github.com/yalue/onnxruntime_go/blob/master/README.md>
