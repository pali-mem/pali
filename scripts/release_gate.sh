#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

echo "==> Release gate"

bash ./scripts/check_docs_freshness.sh
bash ./scripts/dead_code_sweep.sh
bash ./scripts/verify_docs_examples.sh

go test ./internal/... ./pkg/... ./cmd/...
go test -tags integration ./test/integration/...
go test -tags e2e ./test/e2e/...
go build ./cmd/pali

echo "Release gate passed."
