APP=pali
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

.PHONY: run mcp setup build install release-assets test test-integration test-e2e test-all jwt fmt tidy benchmark bench-setup retrieval-quality retrieval-trend check-wiring docs-deps docs-run docs-build docs-freshness dead-code-sweep release-gate

run:
	go run ./cmd/pali -config pali.yaml

mcp:
	go run ./cmd/pali mcp run -config pali.yaml

setup:
	go run ./cmd/setup -skip-model-download

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

bench-setup:
	bash ./scripts/bench_setup.sh

benchmark:
	bash ./scripts/benchmark.sh --fixture $${FIXTURE:-testdata/benchmarks/fixtures/release_memories.json} --eval-set $${EVAL_SET:-testdata/benchmarks/evals/release_curated.json} --backend $${BACKEND:-sqlite}

retrieval-quality:
	bash ./scripts/retrieval_quality.sh --fixture $${FIXTURE:-testdata/benchmarks/fixtures/release_memories.json} --eval-set $${EVAL_SET:-testdata/benchmarks/evals/release_curated.json} --backend $${BACKEND:-sqlite}

retrieval-trend:
	bash ./scripts/retrieval_trend.sh --fixture $${FIXTURE:-testdata/benchmarks/fixtures/release_memories.json} --eval-set $${EVAL_SET:-testdata/benchmarks/evals/release_curated.json} --backend $${BACKEND:-sqlite}

bench-suite:
	python ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.local.json

bench-suite-medium:
	python ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.medium.fast.json

bench-suite-qdrant:
	python ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.qdrant_ollama.json

bench-suite-openrouter:
	python ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.medium.qdrant-openrouter.json

bench-suite-openrouter-parser-graph:
	python ./test/benchmarks/benchmark_suite.py --config test/benchmarks/suites/speed.medium.qdrant-openrouter-parser-graph.json

benchmark-clean:
	rm -rf test/benchmarks/results/*

check-wiring:
	go test ./internal/core/memory ./internal/repository/sqlite -run 'Test(SearchBuildsIterativeQueriesForMultiHopQuestion|SearchWithFiltersAppliesKindFilter|SearchAggregationRouteRespectsMinScore|StoreMarksIndexStateTransitions|StoreMarksIndexStateFailedOnVectorFailure|DeleteMarksIndexStateTombstoned|DeleteMarksIndexStateFailedOnVectorFailure|MemoryRepositoryIndexJobLifecycle)' -count=1

docs-deps:
	python -m pip install -r docs/requirements.txt

docs-run: docs-deps
	mkdocs serve

docs-build: docs-deps
	mkdocs build --strict

docs-freshness:
	bash ./scripts/check_docs_freshness.sh

dead-code-sweep:
	bash ./scripts/dead_code_sweep.sh

release-gate:
	bash ./scripts/release_gate.sh
