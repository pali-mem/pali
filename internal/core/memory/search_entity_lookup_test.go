package memory

import (
	"context"
	"testing"
	"time"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

// batchEntityFactRepoStub adds the optional batch lookup to the per-pair stub
// and counts batch calls separately from per-pair calls.
type batchEntityFactRepoStub struct {
	*retrievalEntityFactRepoStub
	batchCalls int
	batchPairs int
}

func (r *batchEntityFactRepoStub) ListByEntityRelations(
	ctx context.Context,
	tenantID string,
	entities, relations []string,
	limit int,
) ([]domain.EntityFact, error) {
	r.batchCalls++
	perPairCalls := r.listCalls
	out := make([]domain.EntityFact, 0)
	for _, entity := range entities {
		for _, relation := range relations {
			r.batchPairs++
			facts, err := r.ListByEntityRelation(ctx, tenantID, entity, relation, limit)
			if err != nil {
				return nil, err
			}
			out = append(out, facts...)
		}
	}
	r.listCalls = perPairCalls
	return out, nil
}

func entityLookupTestFixture(now time.Time) (*retrievalRepoStub, []domain.EntityFact) {
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"bridge": {
				ID:             "bridge",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindEvent,
				Content:        "Melanie attended a support group in June.",
				Importance:     0.8,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
			"pottery": {
				ID:             "pottery",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Melanie went to a pottery workshop with Caroline.",
				Importance:     0.6,
				LastAccessedAt: now.Add(-2 * time.Hour),
			},
			"stale": {
				ID:             "stale",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Melanie attended a book club.",
				Importance:     0.5,
				LastAccessedAt: now.Add(-3 * time.Hour),
			},
		},
		searchResults: []domain.Memory{},
	}
	facts := []domain.EntityFact{
		{TenantID: "tenant_1", Entity: "melanie", Relation: "event", Value: "support group", MemoryID: "bridge"},
		{TenantID: "tenant_1", Entity: "melanie", Relation: "activity", Value: "pottery", MemoryID: "pottery"},
		{TenantID: "tenant_1", Entity: "caroline", Relation: "event", Value: "pottery workshop", MemoryID: "pottery"},
		{TenantID: "tenant_1", Entity: "melanie", Relation: "event", Value: "book club", MemoryID: "stale", InvalidatedByFactID: "ef_newer"},
	}
	return repo, facts
}

func TestSearchEntityFactLookupsUseBatchRepository(t *testing.T) {
	cases := []struct {
		name  string
		query string
		opts  SearchOptions
	}{
		{name: "graph bridge", query: "When did Melanie and Caroline go to the support group?", opts: SearchOptions{RetrievalKind: SearchRetrievalKindEntity}},
		{name: "entity hint and graph bridge", query: "Which workshop does Melanie like?", opts: SearchOptions{RetrievalKind: SearchRetrievalKindEntity}},
		{name: "aggregation lookup", query: "what activities does melanie do?", opts: SearchOptions{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC()
			perPairRepo, facts := entityLookupTestFixture(now)
			perPair := &retrievalEntityFactRepoStub{facts: facts}
			perPairSvc := NewService(perPairRepo, retrievalTenantRepoStub{}, nil, nil, retrievalScorerStub{}, WithEntityFactRepository(perPair))
			want, err := perPairSvc.SearchWithFilters(context.Background(), "tenant_1", tc.query, 5, tc.opts)
			require.NoError(t, err)
			require.NotEmpty(t, want)

			batchRepo, facts := entityLookupTestFixture(now)
			batch := &batchEntityFactRepoStub{retrievalEntityFactRepoStub: &retrievalEntityFactRepoStub{facts: facts}}
			batchSvc := NewService(batchRepo, retrievalTenantRepoStub{}, nil, nil, retrievalScorerStub{}, WithEntityFactRepository(batch))
			got, err := batchSvc.SearchWithFilters(context.Background(), "tenant_1", tc.query, 5, tc.opts)
			require.NoError(t, err)

			t.Logf("per-pair repository calls=%d; batch repository calls=%d covering %d pairs", perPair.listCalls, batch.batchCalls, batch.batchPairs)
			require.Equal(t, want, got)
			require.Zero(t, batch.listCalls, "batch-capable repository must not receive per-pair lookups")
			require.Equal(t, perPair.listCalls, batch.batchPairs, "batch lookups must cover the same pairs")
			require.Greater(t, perPair.listCalls, batch.batchCalls)
			require.LessOrEqual(t, batch.batchCalls, 2, "at most one batch call per lookup site (entity hint, graph bridge)")
		})
	}
}

func TestListEntityFactsByRelationsFallbackMatchesBatchOrder(t *testing.T) {
	_, facts := entityLookupTestFixture(time.Now().UTC())
	entities := []string{"melanie", "caroline"}
	relations := []string{"event", "activity", "place"}

	perPair := &retrievalEntityFactRepoStub{facts: facts}
	fallbackSvc := NewService(nil, nil, nil, nil, nil, WithEntityFactRepository(perPair))
	want, err := fallbackSvc.listEntityFactsByRelations(context.Background(), "tenant_1", entities, relations, 10)
	require.NoError(t, err)
	require.Equal(t, len(entities)*len(relations), perPair.listCalls)

	batch := &batchEntityFactRepoStub{retrievalEntityFactRepoStub: &retrievalEntityFactRepoStub{facts: facts}}
	batchSvc := NewService(nil, nil, nil, nil, nil, WithEntityFactRepository(batch))
	got, err := batchSvc.listEntityFactsByRelations(context.Background(), "tenant_1", entities, relations, 10)
	require.NoError(t, err)
	require.Equal(t, 1, batch.batchCalls)
	require.Zero(t, batch.listCalls)
	require.Equal(t, want, got)

	// Singleton invalidation is on by default, so the invalidated fact is dropped.
	require.Len(t, got, 3)
	require.Equal(t, []string{"bridge", "pottery", "pottery"}, []string{got[0].MemoryID, got[1].MemoryID, got[2].MemoryID})
}
