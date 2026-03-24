APP=pali
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
PYTHON ?= $(shell command -v python >/dev/null 2>&1 && echo python || command -v python3 >/dev/null 2>&1 && echo python3)
DOCS_VENV ?= .venv/docs
DOCS_PYTHON := $(DOCS_VENV)/bin/python
DOCS_STAMP := $(DOCS_VENV)/.requirements.stamp

.PHONY: init serve run mcp mcp-serve setup build install release-assets test test-integration test-e2e test-all jwt fmt tidy lint benchmark bench-setup retrieval-quality retrieval-trend check-wiring docs-deps docs-run docs-build docs-freshness dead-code-sweep release-gate

init:
	go run ./cmd/pali init -skip-model-download

serve:
	go run ./cmd/pali serve -config pali.yaml

run: serve

mcp-serve:
	go run ./cmd/pali mcp serve -config pali.yaml

mcp: mcp-serve

setup: init

build:
	go build -o bin/$(APP) ./cmd/pali

install: build
	install -d "$(BINDIR)"
	install -m 0755 "bin/$(APP)" "$(BINDIR)/$(APP)"

release-assets:
	bash ./scripts/release_assets.sh --version "$${VERSION:-}"

# Unit tests only (package-level, no tags)
test:
	go test ./internal/... ./pkg/...

# Integration tests (require real DB, etc.)
test-integration:
	go test -tags integration ./test/integration/...

# E2E tests (require running server)
test-e2e:
	go test -tags e2e ./test/e2e/...

# Everything
test-all:
	go test -tags integration,e2e ./...

# Mint a dev JWT token (set TENANT and optionally JWT_SECRET)
jwt:
	go run ./cmd/jwt -tenant $${TENANT:-tenant_1} -secret "$${JWT_SECRET:-}"

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

tidy:
	go mod tidy

lint:
	bash ./scripts/lint.sh

bench-setup:
	bash ./scripts/bench_setup.sh

benchmark:
	bash ./scripts/benchmark.sh --fixture $${FIXTURE:-testdata/benchmarks/fixtures/release_memories.json} --eval-set $${EVAL_SET:-testdata/benchmarks/evals/release_curated.json} --backend $${BACKEND:-sqlite}

retrieval-quality:
	bash ./scripts/retrieval_quality.sh --fixture $${FIXTURE:-testdata/benchmarks/fixtures/release_memories.json} --eval-set $${EVAL_SET:-testdata/benchmarks/evals/release_curated.json} --backend $${BACKEND:-sqlite}

retrieval-trend:
	bash ./scripts/retrieval_trend.sh --fixture $${FIXTURE:-testdata/benchmarks/fixtures/release_memories.json} --eval-set $${EVAL_SET:-testdata/benchmarks/evals/release_curated.json} --backend $${BACKEND:-sqlite}

bench-suite:
	@test -n "$(PYTHON)" || (echo "ERROR: python or python3 is required"; exit 1)
	$(PYTHON) ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.local.json

bench-suite-medium:
	@test -n "$(PYTHON)" || (echo "ERROR: python or python3 is required"; exit 1)
	$(PYTHON) ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.medium.fast.json

bench-suite-qdrant:
	@test -n "$(PYTHON)" || (echo "ERROR: python or python3 is required"; exit 1)
	$(PYTHON) ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.qdrant_ollama.json

bench-suite-openrouter:
	@test -n "$(PYTHON)" || (echo "ERROR: python or python3 is required"; exit 1)
	$(PYTHON) ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.medium.qdrant-openrouter.json

bench-suite-openrouter-parser-graph:
	@test -n "$(PYTHON)" || (echo "ERROR: python or python3 is required"; exit 1)
	$(PYTHON) ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.medium.qdrant-openrouter-parser-graph.json

benchmark-clean:
	rm -rf test/benchmarks/results/*

check-wiring:
	go test ./internal/core/memory ./internal/repository/sqlite -run 'Test(SearchBuildsIterativeQueriesForMultiHopQuestion|SearchWithFiltersAppliesKindFilter|SearchAggregationRouteRespectsMinScore|StoreMarksIndexStateTransitions|StoreMarksIndexStateFailedOnVectorFailure|DeleteMarksIndexStateTombstoned|DeleteMarksIndexStateFailedOnVectorFailure|MemoryRepositoryIndexJobLifecycle)' -count=1

docs-deps:
	@$(MAKE) $(DOCS_STAMP)

$(DOCS_PYTHON):
	@test -n "$(PYTHON)" || (echo "ERROR: python or python3 is required"; exit 1)
	$(PYTHON) -m venv "$(DOCS_VENV)"

$(DOCS_STAMP): docs/requirements.txt | $(DOCS_PYTHON)
	$(DOCS_PYTHON) -m pip install -r docs/requirements.txt
	touch "$(DOCS_STAMP)"

docs-run: docs-deps
	$(DOCS_PYTHON) -m mkdocs serve

docs-build: docs-deps
	$(DOCS_PYTHON) -m mkdocs build --strict

docs-freshness:
	bash ./scripts/check_docs_freshness.sh

dead-code-sweep:
	bash ./scripts/dead_code_sweep.sh

release-gate:
	bash ./scripts/release_gate.sh
