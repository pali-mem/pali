package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/domain"
)

type retrievalRepoStub struct {
	memoriesByID         map[string]domain.Memory
	searchResults        []domain.Memory
	searchResultsByQuery map[string][]domain.Memory
	searchedQueries      []string
	filteredSearchCalls  int
	touched              []string
}

func (r *retrievalRepoStub) Store(ctx context.Context, m domain.Memory) (domain.Memory, error) {
	return m, nil
}
func (r *retrievalRepoStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	return nil
}
func (r *retrievalRepoStub) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	r.searchedQueries = append(r.searchedQueries, query)
	if r.searchResultsByQuery != nil {
		if results, ok := r.searchResultsByQuery[query]; ok {
			if len(results) <= topK {
				return results, nil
			}
			return results[:topK], nil
		}
	}
	if len(r.searchResults) <= topK {
		return r.searchResults, nil
	}
	return r.searchResults[:topK], nil
}

func (r *retrievalRepoStub) SearchWithFilters(
	ctx context.Context,
	tenantID, query string,
	topK int,
	filters domain.MemorySearchFilters,
) ([]domain.Memory, error) {
	r.filteredSearchCalls++
	results, err := r.Search(ctx, tenantID, query, topK)
	if err != nil {
		return nil, err
	}
	if len(filters.Kinds) == 0 && len(filters.Tiers) == 0 {
		return results, nil
	}
	kindFilter := make(map[domain.MemoryKind]struct{}, len(filters.Kinds))
	for _, kind := range filters.Kinds {
		kindFilter[kind] = struct{}{}
	}
	tierFilter := make(map[domain.MemoryTier]struct{}, len(filters.Tiers))
	for _, tier := range filters.Tiers {
		tierFilter[tier] = struct{}{}
	}
	filtered := make([]domain.Memory, 0, len(results))
	for _, memory := range results {
		if len(kindFilter) > 0 {
			if _, ok := kindFilter[memory.Kind]; !ok {
				continue
			}
		}
		if len(tierFilter) > 0 {
			if _, ok := tierFilter[memory.Tier]; !ok {
				continue
			}
		}
		filtered = append(filtered, memory)
		if len(filtered) >= topK {
			break
		}
	}
	return filtered, nil
}
func (r *retrievalRepoStub) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]domain.Memory, error) {
	out := make([]domain.Memory, 0, len(ids))
	for _, id := range ids {
		if m, ok := r.memoriesByID[id]; ok && m.TenantID == tenantID {
			out = append(out, m)
		}
	}
	return out, nil
}
func (r *retrievalRepoStub) Touch(ctx context.Context, tenantID string, ids []string) error {
	r.touched = append([]string{}, ids...)
	return nil
}

func (r *retrievalRepoStub) ListBySourceTurnHash(
	ctx context.Context,
	tenantID, sourceTurnHash string,
	limit int,
) ([]domain.Memory, error) {
	out := make([]domain.Memory, 0, limit)
	for _, memory := range r.memoriesByID {
		if memory.TenantID != tenantID || memory.SourceTurnHash != sourceTurnHash {
			continue
		}
		out = append(out, memory)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

type retrievalTenantRepoStub struct{}

func (retrievalTenantRepoStub) Create(ctx context.Context, t domain.Tenant) (domain.Tenant, error) {
	return t, nil
}
func (retrievalTenantRepoStub) Exists(ctx context.Context, tenantID string) (bool, error) {
	return true, nil
}
func (retrievalTenantRepoStub) MemoryCount(ctx context.Context, tenantID string) (int64, error) {
	return 0, nil
}
func (retrievalTenantRepoStub) List(ctx context.Context, limit int) ([]domain.Tenant, error) {
	return []domain.Tenant{}, nil
}

type retrievalVectorStub struct {
	candidates []domain.VectorstoreCandidate
}

func (v retrievalVectorStub) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	return nil
}
func (v retrievalVectorStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	return nil
}
func (v retrievalVectorStub) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	if len(v.candidates) <= topK {
		return v.candidates, nil
	}
	return v.candidates[:topK], nil
}

type retrievalEmbedderStub struct{}

func (retrievalEmbedderStub) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{0.2, 0.3, 0.4}, nil
}

type retrievalScorerStub struct{}

func (retrievalScorerStub) Score(ctx context.Context, text string) (float64, error) {
	return 0.6, nil
}

type retrievalEntityFactRepoStub struct {
	facts []domain.EntityFact
}

func (r *retrievalEntityFactRepoStub) Store(ctx context.Context, fact domain.EntityFact) (domain.EntityFact, error) {
	r.facts = append(r.facts, fact)
	return fact, nil
}

func (r *retrievalEntityFactRepoStub) ListByEntityRelation(
	ctx context.Context,
	tenantID, entity, relation string,
	limit int,
) ([]domain.EntityFact, error) {
	out := make([]domain.EntityFact, 0, len(r.facts))
	for _, fact := range r.facts {
		if fact.TenantID != tenantID || fact.Entity != entity || fact.Relation != relation {
			continue
		}
		out = append(out, fact)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func TestSearchRanksByWMRAndTouches(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"m1": {
				ID:             "m1",
				TenantID:       "tenant_1",
				Content:        "older but relevant",
				Importance:     0.3,
				LastAccessedAt: now.Add(-72 * time.Hour),
			},
			"m2": {
				ID:             "m2",
				TenantID:       "tenant_1",
				Content:        "newer and important",
				Importance:     0.9,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
	}

	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "m1", Similarity: 0.95},
			{MemoryID: "m2", Similarity: 0.70},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, vector, retrievalEmbedderStub{}, retrievalScorerStub{})
	results, err := svc.Search(context.Background(), "tenant_1", "query", 2)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "m2", results[0].ID)
	require.Equal(t, "m1", results[1].ID)
	require.Equal(t, []string{"m2", "m1"}, repo.touched)
}

func TestSearchFallsBackToLexicalOnly(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"m1": {
				ID:             "m1",
				TenantID:       "tenant_1",
				Content:        "terse responses preferred",
				Importance:     0.5,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
			"m2": {
				ID:             "m2",
				TenantID:       "tenant_1",
				Content:        "long form explanations are okay",
				Importance:     0.5,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
		searchResults: []domain.Memory{
			{ID: "m1", TenantID: "tenant_1"},
			{ID: "m2", TenantID: "tenant_1"},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, nil, nil, retrievalScorerStub{})
	results, err := svc.Search(context.Background(), "tenant_1", "how does the user like replies", 2)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "m1", results[0].ID)
	require.Equal(t, "m2", results[1].ID)
	require.Equal(t, []string{"m1", "m2"}, repo.touched)
}

func TestSearchWithFiltersAppliesTierAndMinScore(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"m1": {
				ID:             "m1",
				TenantID:       "tenant_1",
				Tier:           domain.MemoryTierEpisodic,
				Content:        "semantic preference",
				Importance:     0.2,
				LastAccessedAt: now.Add(-72 * time.Hour),
			},
			"m2": {
				ID:             "m2",
				TenantID:       "tenant_1",
				Tier:           domain.MemoryTierEpisodic,
				Content:        "episodic event",
				Importance:     0.9,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
	}

	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "m2", Similarity: 0.95},
			{MemoryID: "m1", Similarity: 0.10},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, vector, retrievalEmbedderStub{}, retrievalScorerStub{})
	results, err := svc.SearchWithFilters(context.Background(), "tenant_1", "episodic event", 5, SearchOptions{
		MinScore: 0.5,
		Tiers:    []domain.MemoryTier{domain.MemoryTierEpisodic},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "m2", results[0].ID)
	require.Equal(t, []string{"m2"}, repo.touched)
}

func TestSearchWithFiltersCanDisableTouch(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"m1": {
				ID:             "m1",
				TenantID:       "tenant_1",
				Content:        "semantic preference",
				Importance:     0.9,
				LastAccessedAt: now.Add(-2 * time.Hour),
			},
		},
	}

	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "m1", Similarity: 0.95},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, vector, retrievalEmbedderStub{}, retrievalScorerStub{})
	results, err := svc.SearchWithFilters(context.Background(), "tenant_1", "query", 5, SearchOptions{
		DisableTouch: true,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Empty(t, repo.touched)
}

func TestSearchWithFiltersAppliesKindFilter(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"m1": {
				ID:             "m1",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "raw turn",
				Importance:     0.8,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
			"m2": {
				ID:             "m2",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "observation turn",
				Importance:     0.8,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
	}
	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "m1", Similarity: 0.95},
			{MemoryID: "m2", Similarity: 0.95},
		},
	}
	svc := NewService(repo, retrievalTenantRepoStub{}, vector, retrievalEmbedderStub{}, retrievalScorerStub{})
	results, err := svc.SearchWithFilters(context.Background(), "tenant_1", "query", 5, SearchOptions{
		Kinds: []domain.MemoryKind{domain.MemoryKindObservation},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "m2", results[0].ID)
	require.GreaterOrEqual(t, repo.filteredSearchCalls, 1)
}

func TestSearchPrefersCanonicalUnitsOverRawTurnsByDefault(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"raw": {
				ID:             "raw",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "Alex: I live in Austin.",
				Importance:     0.8,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
			"obs": {
				ID:             "obs",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Alex lives in Austin.",
				Importance:     0.7,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
	}
	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "raw", Similarity: 0.95},
			{MemoryID: "obs", Similarity: 0.95},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, vector, retrievalEmbedderStub{}, retrievalScorerStub{})
	results, err := svc.Search(context.Background(), "tenant_1", "where does Alex live?", 2)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "obs", results[0].ID)
	require.Equal(t, "raw", results[1].ID)
}

func TestSearchWithQueryRoutingBoostsTemporalKinds(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"raw": {
				ID:             "raw",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "[dialog:D1:2] Alex: We met last month.",
				Importance:     0.8,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
			"event": {
				ID:             "event",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindEvent,
				Content:        "Alex at 2 May, 2023: We met last month.",
				Importance:     0.8,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
	}
	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "raw", Similarity: 0.95},
			{MemoryID: "event", Similarity: 0.95},
		},
	}
	svc := NewService(
		repo,
		retrievalTenantRepoStub{},
		vector,
		retrievalEmbedderStub{},
		retrievalScorerStub{},
		StructuredMemoryOptions{},
	)
	results, err := svc.Search(context.Background(), "tenant_1", "when did they meet?", 5)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "event", results[0].ID)
}

func TestSearchWithMatchRankingPrioritizesQueryOverlap(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"m1": {
				ID:             "m1",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Alice prefers vegan meals and avoids dairy products.",
				Importance:     0.6,
				LastAccessedAt: now.Add(-2 * time.Hour),
			},
			"m2": {
				ID:             "m2",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Bob likes bicycle rides on weekends near the lake.",
				Importance:     0.6,
				LastAccessedAt: now.Add(-2 * time.Hour),
			},
		},
	}
	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "m1", Similarity: 0.8},
			{MemoryID: "m2", Similarity: 0.8},
		},
	}
	svc := NewService(
		repo,
		retrievalTenantRepoStub{},
		vector,
		retrievalEmbedderStub{},
		retrievalScorerStub{},
		RankingOptions{
			Algorithm: "match",
			Match: MatchWeights{
				Recency:      0.0,
				Relevance:    0.2,
				Importance:   0.0,
				QueryOverlap: 0.8,
				Routing:      0.0,
			},
		},
	)
	results, err := svc.Search(context.Background(), "tenant_1", "what food does alice avoid dairy", 5)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "m1", results[0].ID)
}

func TestSearchCanonicalPriorityKeepsRawTurnsBehindFacts(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"raw_1": {
				ID:             "raw_1",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "On 20 Oct 2023, Alex said that yeah, totally agree.",
				Importance:     0.8,
				LastAccessedAt: now.Add(-30 * time.Minute),
			},
			"raw_2": {
				ID:             "raw_2",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "On 22 Oct 2023, Alex said that thanks so much!",
				Importance:     0.8,
				LastAccessedAt: now.Add(-30 * time.Minute),
			},
			"raw_3": {
				ID:             "raw_3",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "On 25 Oct 2023, Alex said that sounds good.",
				Importance:     0.8,
				LastAccessedAt: now.Add(-30 * time.Minute),
			},
			"obs": {
				ID:              "obs",
				TenantID:        "tenant_1",
				Kind:            domain.MemoryKindObservation,
				Content:         "Alex studies counseling and psychology.",
				QueryViewText:   "what field does alex study",
				CanonicalKey:    "canon_obs",
				SourceFactIndex: 3,
				Importance:      0.7,
				LastAccessedAt:  now.Add(-2 * time.Hour),
			},
		},
	}
	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "raw_1", Similarity: 0.95},
			{MemoryID: "raw_2", Similarity: 0.94},
			{MemoryID: "raw_3", Similarity: 0.93},
			{MemoryID: "obs", Similarity: 0.90},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, vector, retrievalEmbedderStub{}, retrievalScorerStub{})
	results, err := svc.Search(context.Background(), "tenant_1", "what field does alex study", 4)
	require.NoError(t, err)
	require.Len(t, results, 4)
	require.Equal(t, "obs", results[0].ID)
}

func TestSearchDemotesLowEvidenceFreshMemoryForSingleHop(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"weak": {
				ID:             "weak",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Completely unrelated status update.",
				Importance:     1.0,
				LastAccessedAt: now.Add(-10 * time.Minute),
			},
			"strong": {
				ID:             "strong",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Alice prefers vegan meals and avoids dairy.",
				Importance:     0.1,
				LastAccessedAt: now.Add(-72 * time.Hour),
			},
		},
	}
	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "weak", Similarity: 0.18},
			{MemoryID: "strong", Similarity: 0.62},
		},
	}
	svc := NewService(repo, retrievalTenantRepoStub{}, vector, retrievalEmbedderStub{}, retrievalScorerStub{})
	results, err := svc.Search(context.Background(), "tenant_1", "what food does alice avoid dairy", 5)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "strong", results[0].ID)
}

func TestSearchAggregationRouteUsesEntityFactsWhenAvailable(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"activity_1": {
				ID:             "activity_1",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Melanie enjoys camping.",
				Importance:     0.7,
				LastAccessedAt: now.Add(-2 * time.Hour),
			},
			"activity_2": {
				ID:             "activity_2",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Melanie practices pottery.",
				Importance:     0.7,
				LastAccessedAt: now.Add(-2 * time.Hour),
			},
			"recent_generic": {
				ID:             "recent_generic",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "Totally agree, let's catch up tomorrow.",
				Importance:     0.7,
				LastAccessedAt: now.Add(-2 * time.Hour),
			},
		},
		searchResults: []domain.Memory{
			{ID: "recent_generic", TenantID: "tenant_1"},
		},
	}
	entityFacts := &retrievalEntityFactRepoStub{
		facts: []domain.EntityFact{
			{TenantID: "tenant_1", Entity: "melanie", Relation: "activity", Value: "camping", MemoryID: "activity_1"},
			{TenantID: "tenant_1", Entity: "melanie", Relation: "activity", Value: "pottery", MemoryID: "activity_2"},
		},
	}

	svc := NewService(
		repo,
		retrievalTenantRepoStub{},
		nil,
		nil,
		retrievalScorerStub{},
		StructuredMemoryOptions{},
		WithEntityFactRepository(entityFacts),
	)
	results, err := svc.Search(context.Background(), "tenant_1", "what activities does melanie do?", 5)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "activity_1", results[0].ID)
	require.Equal(t, "activity_2", results[1].ID)
	require.Equal(t, []string{"activity_1", "activity_2"}, repo.touched)
}

func TestSearchAggregationRouteRespectsMinScore(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"activity_1": {
				ID:             "activity_1",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Melanie enjoys camping.",
				Importance:     0.7,
				LastAccessedAt: now.Add(-2 * time.Hour),
			},
		},
	}
	entityFacts := &retrievalEntityFactRepoStub{
		facts: []domain.EntityFact{
			{TenantID: "tenant_1", Entity: "melanie", Relation: "activity", Value: "camping", MemoryID: "activity_1"},
		},
	}

	svc := NewService(
		repo,
		retrievalTenantRepoStub{},
		nil,
		nil,
		retrievalScorerStub{},
		WithEntityFactRepository(entityFacts),
	)
	results, err := svc.SearchWithFilters(context.Background(), "tenant_1", "what activities does melanie do?", 5, SearchOptions{
		MinScore: 0.95,
	})
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestSearchExpandsGroundedRawTurnContextAfterFactHit(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"fact": {
				ID:             "fact",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "On 8 May 2023, Caroline attended an LGBTQ support group.",
				SourceTurnHash: "turn_1",
				Importance:     0.8,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
			"raw": {
				ID:             "raw",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "[time:1:56 pm on 8 May, 2023] Caroline: I went to an LGBTQ support group yesterday and it was powerful.",
				SourceTurnHash: "turn_1",
				Importance:     0.7,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
	}
	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "fact", Similarity: 0.95},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, vector, retrievalEmbedderStub{}, retrievalScorerStub{})
	results, err := svc.Search(context.Background(), "tenant_1", "when did Caroline go to the LGBTQ support group", 2)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "fact", results[0].ID)
	require.Equal(t, "raw", results[1].ID)
}

func TestSearchDoesNotExpandGroundedContextForSingleHopQuery(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"fact": {
				ID:             "fact",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Caroline prefers brief daily running sessions.",
				SourceTurnHash: "turn_1",
				Importance:     0.8,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
			"raw": {
				ID:             "raw",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "Caroline: I prefer brief daily running sessions.",
				SourceTurnHash: "turn_1",
				Importance:     0.7,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
	}
	vector := retrievalVectorStub{
		candidates: []domain.VectorstoreCandidate{
			{MemoryID: "fact", Similarity: 0.95},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, vector, retrievalEmbedderStub{}, retrievalScorerStub{})
	results, err := svc.Search(context.Background(), "tenant_1", "what running habit does caroline prefer", 2)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "fact", results[0].ID)
}

func TestSearchBuildsMultipleQueriesForTemporalQuestion(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"m1": {
				ID:             "m1",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Alex moved to Austin in 2024.",
				Importance:     0.7,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
		searchResults: []domain.Memory{
			{ID: "m1", TenantID: "tenant_1"},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, nil, nil, retrievalScorerStub{})
	_, err := svc.Search(context.Background(), "tenant_1", "when did Alex move to Austin?", 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(repo.searchedQueries), 2)
}

func TestSearchBuildsIterativeQueriesForMultiHopQuestion(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"m1": {
				ID:             "m1",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Alex met Jordan in Seattle.",
				Importance:     0.7,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
		searchResultsByQuery: map[string][]domain.Memory{
			"who met jordan before moving to austin": {
				{ID: "m1", TenantID: "tenant_1", Kind: domain.MemoryKindObservation, Content: "Alex met Jordan in Seattle."},
			},
		},
	}

	svc := NewService(repo, retrievalTenantRepoStub{}, nil, nil, retrievalScorerStub{})
	_, err := svc.Search(context.Background(), "tenant_1", "who met Jordan before moving to Austin?", 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(repo.searchedQueries), 2)
}

func TestSearchUsesEntityHintCandidatesForSingleHopQuestion(t *testing.T) {
	now := time.Now().UTC()
	repo := &retrievalRepoStub{
		memoriesByID: map[string]domain.Memory{
			"reason_fact": {
				ID:             "reason_fact",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindObservation,
				Content:        "Melanie started running to de-stress and protect her health.",
				Importance:     0.6,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
			"generic": {
				ID:             "generic",
				TenantID:       "tenant_1",
				Kind:           domain.MemoryKindRawTurn,
				Content:        "Melanie: Yeah totally.",
				Importance:     0.6,
				LastAccessedAt: now.Add(-1 * time.Hour),
			},
		},
		searchResults: []domain.Memory{
			{ID: "generic", TenantID: "tenant_1", Kind: domain.MemoryKindRawTurn, Content: "Melanie: Yeah totally."},
		},
	}
	entityFacts := &retrievalEntityFactRepoStub{
		facts: []domain.EntityFact{
			{
				TenantID: "tenant_1",
				Entity:   "melanie",
				Relation: "activity",
				Value:    "running",
				MemoryID: "reason_fact",
			},
		},
	}
	svc := NewService(
		repo,
		retrievalTenantRepoStub{},
		nil,
		nil,
		retrievalScorerStub{},
		WithEntityFactRepository(entityFacts),
	)
	results, err := svc.Search(context.Background(), "tenant_1", "What is Melanie's reason for getting into running?", 3)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "reason_fact", results[0].ID)
}

func TestFuseCandidatesByRRFDedupesRepeatedVariantMatches(t *testing.T) {
	dense := []domain.VectorstoreCandidate{
		{MemoryID: "generic", Similarity: 0.20},
		{MemoryID: "generic", Similarity: 0.20},
		{MemoryID: "specific", Similarity: 0.75},
	}
	lexical := []lexicalCandidate{
		{Memory: domain.Memory{ID: "generic"}, Score: 0.22},
		{Memory: domain.Memory{ID: "generic"}, Score: 0.21},
		{Memory: domain.Memory{ID: "specific"}, Score: 0.68},
	}
	ids, signals := fuseCandidatesByRRF(dense, lexical, 3)
	require.Len(t, ids, 2)
	require.Equal(t, "generic", ids[0])
	require.Equal(t, "specific", ids[1])
	require.Greater(t, signals["specific"].DenseScore, signals["generic"].DenseScore)
	require.Greater(t, signals["generic"].LexicalScore, 0.20)
	require.Greater(t, signals["specific"].RRFScore, 0.0)
}
