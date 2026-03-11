#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

PYTHON_BIN="${PYTHON_BIN:-}"
if [[ -z "$PYTHON_BIN" ]]; then
  if command -v python3 >/dev/null 2>&1; then
    PYTHON_BIN="python3"
  elif command -v python >/dev/null 2>&1; then
    PYTHON_BIN="python"
  else
    echo "ERROR: Python interpreter not found (python3 or python required)"
    exit 1
  fi
fi

tmp_dir="$(mktemp -d)"
cleanup_api() {
  :
}
cleanup() {
  cleanup_api
  rm -rf "$tmp_dir"
}
trap cleanup EXIT

setup_cfg="$tmp_dir/pali.yaml"
rendered_cfg="$tmp_dir/release.yaml"
sqlite_path="$tmp_dir/release.sqlite"

to_host_path() {
  local value="$1"
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$value"
    return
  fi
  printf '%s\n' "$value"
}

echo "==> Verifying documented setup flow"
go run ./cmd/setup -config "$setup_cfg" -skip-model-download -skip-runtime-check -skip-ollama-check

echo "==> Verifying documented config render flow"
sqlite_host_path="$(to_host_path "$sqlite_path")"
go run ./cmd/configrender \
  -profile test/config/providers/mock.yaml \
  -out "$rendered_cfg" \
  -host 127.0.0.1 \
  -port 18081 \
  -vector-backend sqlite \
  -sqlite-dsn "file:${sqlite_host_path}?cache=shared"

echo "==> Verifying documented API startup flow"
api_bin="$tmp_dir/pali-docs-example"
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    api_bin="${api_bin}.exe"
    ;;
esac
go build -o "$api_bin" ./cmd/pali
if ! "$PYTHON_BIN" - <<'PY' "$(to_host_path "$api_bin")" "$(to_host_path "$rendered_cfg")" "$(to_host_path "$tmp_dir/api.log")"
from __future__ import annotations

import subprocess
import sys
import time
import urllib.request

binary = sys.argv[1]
cfg = sys.argv[2]
log_path = sys.argv[3]
with open(log_path, "wb") as log:
    proc = subprocess.Popen(
        [binary, "-config", cfg],
        stdout=log,
        stderr=subprocess.STDOUT,
    )
    try:
        deadline = time.time() + 60
        while time.time() < deadline:
            try:
                with urllib.request.urlopen("http://127.0.0.1:18081/health", timeout=1) as resp:
                    if resp.status == 200:
                        break
            except Exception:
                time.sleep(0.25)
        else:
            raise SystemExit(f"API example failed to become healthy; see {log_path}")
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=5)
PY
then
  cat "$tmp_dir/api.log" || true
  exit 1
fi

echo "Docs examples executed successfully."
echo "MCP startup coverage remains in go test -tags e2e ./test/e2e/..."
