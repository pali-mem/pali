$ErrorActionPreference = "Stop"

go test ./internal/core/memory ./internal/repository/sqlite `
  -run "Test(SearchBuildsIterativeQueriesForMultiHopQuestion|SearchWithFiltersAppliesKindFilter|SearchAggregationRouteRespectsMinScore|StoreMarksIndexStateTransitions|StoreMarksIndexStateFailedOnVectorFailure|DeleteMarksIndexStateTombstoned|DeleteMarksIndexStateFailedOnVectorFailure|MemoryRepositoryIndexJobLifecycle)" `
  -count=1
