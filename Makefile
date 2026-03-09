APP=pali

.PHONY: run mcp setup build test test-integration test-e2e test-all jwt fmt tidy benchmark bench-setup retrieval-quality retrieval-trend check-wiring

run:
	go run ./cmd/pali -config pali.yaml

mcp:
	go run ./cmd/pali mcp run -config pali.yaml

setup:
	go run ./cmd/setup -skip-model-download

build:
	go build -o bin/$(APP) ./cmd/pali

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
	./scripts/bench_setup.sh

benchmark:
	./scripts/benchmark.sh --fixture $${FIXTURE:-test/fixtures/memories.json} --backend $${BACKEND:-sqlite}

retrieval-quality:
	./scripts/retrieval_quality.sh --fixture $${FIXTURE:-test/fixtures/memories.json} --backend $${BACKEND:-sqlite}

retrieval-trend:
	./scripts/retrieval_trend.sh --fixture $${FIXTURE:-test/fixtures/memories.json} --backend $${BACKEND:-sqlite}

check-wiring:
	go test ./internal/core/memory ./internal/repository/sqlite -run 'Test(SearchBuildsIterativeQueriesForMultiHopQuestion|SearchWithFiltersAppliesKindFilter|SearchAggregationRouteRespectsMinScore|StoreMarksIndexStateTransitions|StoreMarksIndexStateFailedOnVectorFailure|DeleteMarksIndexStateTombstoned|DeleteMarksIndexStateFailedOnVectorFailure|MemoryRepositoryIndexJobLifecycle)' -count=1
