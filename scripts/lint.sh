#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ -x "${HOME}/go/bin/golangci-lint" ]]; then
  LINTER="${HOME}/go/bin/golangci-lint"
elif command -v golangci-lint >/dev/null 2>&1; then
  LINTER="$(command -v golangci-lint)"
else
  cat >&2 <<'EOF'
ERROR: golangci-lint is not installed.

Install with:
  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
EOF
  exit 1
fi

cd "${ROOT}"
exec "${LINTER}" run
