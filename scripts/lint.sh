#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Single source of truth for the linter version (local and CI).
# The linter must be built with a Go version >= the one in go.mod, so it is
# installed with the local toolchain into bin/tools rather than taken from PATH.
GOLANGCI_LINT_VERSION="v2.11.4"
TOOLS_DIR="${ROOT}/bin/tools"
LINTER="${TOOLS_DIR}/golangci-lint-${GOLANGCI_LINT_VERSION}"

if [[ ! -x "${LINTER}" ]]; then
  echo "Installing golangci-lint ${GOLANGCI_LINT_VERSION} into ${TOOLS_DIR}" >&2
  mkdir -p "${TOOLS_DIR}"
  GOBIN="${TOOLS_DIR}" go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}"
  mv "${TOOLS_DIR}/golangci-lint" "${LINTER}"
fi

cd "${ROOT}"
exec "${LINTER}" run "$@"
