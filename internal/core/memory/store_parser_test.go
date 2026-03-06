package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/domain"
)

type countingBatchEmbedder struct {
	embedCalls   int
	batchCalls   int
	lastBatchLen int
	batchErr     error
}

func (e *countingBatchEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.embedCalls++
	return []float32{1, 0, 0}, nil
}

func (e *countingBatchEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	e.batchCalls++
	e.lastBatchLen = len(texts)
	if e.batchErr != nil {
		return nil, e.batchErr
	}
	out := make([][]float32, 0, len(texts))
	for i := range texts {
		out = append(out, []float32{float32(i + 1), 0, 0})
	}
	return out, nil
}

type parserStub struct {
	facts []ParsedFact
	err   error
}

func (p parserStub) Parse(_ context.Context, _ string, maxFacts int) ([]ParsedFact, error) {
	if p.err != nil {
		return nil, p.err
	}
	if maxFacts <= 0 || len(p.facts) <= maxFacts {
		return append([]ParsedFact{}, p.facts...), nil
	}
	return append([]ParsedFact{}, p.facts[:maxFacts]...), nil
}

type countingSearchRepoStub struct {
	structuredRepoStub
	searchCalls int
}

func (r *countingSearchRepoStub) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	r.searchCalls++
	return r.structuredRepoStub.Search(ctx, tenantID, query, topK)
}

type parserEntityFactRepoStub struct {
	stored []domain.EntityFact
}

func (r *parserEntityFactRepoStub) Store(ctx context.Context, fact domain.EntityFact) (domain.EntityFact, error) {
	r.stored = append(r.stored, fact)
	return fact, nil
}

func (r *parserEntityFactRepoStub) StoreBatch(ctx context.Context, facts []domain.EntityFact) ([]domain.EntityFact, error) {
	r.stored = append(r.stored, facts...)
	return facts, nil
}

func (r *parserEntityFactRepoStub) ListByEntityRelation(
	ctx context.Context,
	tenantID, entity, relation string,
	limit int,
) ([]domain.EntityFact, error) {
	out := make([]domain.EntityFact, 0, len(r.stored))
	for _, fact := range r.stored {
		if fact.TenantID != tenantID || fact.Entity != entity || fact.Relation != relation {
			continue
		}
		out = append(out, fact)
	}
	if limit > 0 && len(out) > limit {
		return out[:limit], nil
	}
	return out, nil
}

func TestStoreWithParserCanSkipRawTurn(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content: "Alice avoids dairy products.",
					Kind:    domain.MemoryKindObservation,
					Tags:    []string{"preference"},
				},
			},
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice: I avoid dairy products.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindObservation, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Equal(t, domain.MemoryKindObservation, repo.stored[0].Kind)
	require.Equal(t, domain.MemoryCreatedBySystem, repo.stored[0].CreatedBy)
}

func TestStoreWithParserWritesEntityFacts(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	entityRepo := &parserEntityFactRepoStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content:  "Melanie enjoys camping.",
					Kind:     domain.MemoryKindObservation,
					Entity:   "Melanie",
					Relation: "activity",
					Value:    "camping",
				},
			},
		}),
		WithEntityFactRepository(entityRepo),
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Melanie: I enjoy camping.",
	})
	require.NoError(t, err)
	require.Len(t, entityRepo.stored, 1)
	require.Equal(t, "tenant_1", entityRepo.stored[0].TenantID)
	require.Equal(t, "melanie", entityRepo.stored[0].Entity)
	require.Equal(t, "activity", entityRepo.stored[0].Relation)
	require.Equal(t, "camping", entityRepo.stored[0].Value)
	require.NotEmpty(t, entityRepo.stored[0].MemoryID)
}

func TestStoreBatchWithParserWritesEntityFacts(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	embedder := &countingBatchEmbedder{}
	entityRepo := &parserEntityFactRepoStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedder,
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content:  "Melanie enjoys camping.",
					Kind:     domain.MemoryKindObservation,
					Entity:   "Melanie",
					Relation: "activity",
					Value:    "camping",
				},
			},
		}),
		WithEntityFactRepository(entityRepo),
	)

	_, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "Melanie: I enjoy camping.", Kind: domain.MemoryKindRawTurn},
		{TenantID: "tenant_1", Content: "Melanie: I enjoy camping again.", Kind: domain.MemoryKindRawTurn},
	})
	require.NoError(t, err)
	require.Len(t, entityRepo.stored, 1)
	require.NotEmpty(t, entityRepo.stored[0].MemoryID)
}

func TestStoreWithParserDedupesExistingFact(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.60,
			UpdateThreshold: 0.90,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content: "Alice avoids dairy products.",
					Kind:    domain.MemoryKindObservation,
					Tags:    []string{"preference"},
				},
			},
		}),
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice: I avoid dairy products.",
	})
	require.NoError(t, err)
	_, err = svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice: I avoid dairy products.",
	})
	require.NoError(t, err)
	require.Len(t, repo.stored, 1)
}

func TestStoreWithParserSkipsLegacyStructuredDualWrite(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		structuredEmbedderStub{},
		scorerStub{},
		StructuredMemoryOptions{
			Enabled:               true,
			DualWriteObservations: true,
			DualWriteEvents:       true,
			MaxObservations:       3,
		},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    true,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content: "Alice avoids dairy products.",
					Kind:    domain.MemoryKindObservation,
					Tags:    []string{"preference"},
				},
			},
		}),
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:1:56 pm on 8 May, 2023] Alice: I avoid dairy products.",
	})
	require.NoError(t, err)

	require.Len(t, repo.stored, 2)
	require.Equal(t, domain.MemoryKindRawTurn, repo.stored[0].Kind)
	require.Equal(t, domain.MemoryKindObservation, repo.stored[1].Kind)
	require.Equal(t, "parser", repo.stored[1].Source)
	for _, memory := range repo.stored {
		require.NotEqual(t, "observation", memory.Source)
		require.NotEqual(t, "event", memory.Source)
		require.NotContains(t, memory.Source, ":observation")
		require.NotContains(t, memory.Source, ":event")
	}
}

func TestStoreWithParserFallsBackToHeuristicOnPrimaryFailure(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			err: errors.New("ollama parser timeout"),
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice is vegetarian and avoids dairy products.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindObservation, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Equal(t, domain.MemoryKindObservation, repo.stored[0].Kind)
	require.Equal(t, domain.MemoryCreatedBySystem, repo.stored[0].CreatedBy)
}

func TestStoreWithParserUsesBatchEmbeddingWhenAvailable(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	embedder := &countingBatchEmbedder{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedder,
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    true,
			MaxFacts:        5,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{Content: "Alice likes tea.", Kind: domain.MemoryKindObservation},
				{Content: "Alice moved to Austin.", Kind: domain.MemoryKindEvent},
			},
		}),
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice: I like tea and I moved to Austin.",
	})
	require.NoError(t, err)
	require.Equal(t, 1, embedder.batchCalls)
	require.Equal(t, 3, embedder.lastBatchLen)
	require.Equal(t, 0, embedder.embedCalls)
	require.Len(t, repo.stored, 3)
	require.Len(t, vector.upserted, 3)
}

func TestStoreWithParserFallsBackToSequentialEmbedWhenBatchFails(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	embedder := &countingBatchEmbedder{batchErr: errors.New("batch unavailable")}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedder,
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    true,
			MaxFacts:        5,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{Content: "Alice likes tea.", Kind: domain.MemoryKindObservation},
				{Content: "Alice moved to Austin.", Kind: domain.MemoryKindEvent},
			},
		}),
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice: I like tea and I moved to Austin.",
	})
	require.NoError(t, err)
	require.Equal(t, 1, embedder.batchCalls)
	require.Equal(t, 3, embedder.embedCalls)
	require.Len(t, repo.stored, 3)
	require.Len(t, vector.upserted, 3)
}

func TestStoreBatchUsesBatchEmbedForNonParserInputs(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	embedder := &countingBatchEmbedder{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedder,
		scorerStub{},
	)

	stored, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "Alice likes tea."},
		{TenantID: "tenant_1", Content: "Alice likes hiking."},
		{TenantID: "tenant_1", Content: "Alice likes running."},
	})
	require.NoError(t, err)
	require.Len(t, stored, 3)
	require.Equal(t, 1, embedder.batchCalls)
	require.Equal(t, 3, embedder.lastBatchLen)
	require.Equal(t, 0, embedder.embedCalls)
	require.Len(t, repo.stored, 3)
	require.Len(t, vector.upserted, 3)
	require.Equal(t, 1, vector.batchCalls)
}

func TestStoreBatchMixedParserAndNonParser(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	embedder := &countingBatchEmbedder{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedder,
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    true,
			MaxFacts:        5,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{Content: "Alice likes tea.", Kind: domain.MemoryKindObservation},
			},
		}),
	)

	stored, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "Alice: I like tea.", Kind: domain.MemoryKindRawTurn},
		{TenantID: "tenant_1", Content: "Alice lives in Austin.", Kind: domain.MemoryKindObservation},
	})
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.Equal(t, domain.MemoryKindRawTurn, stored[0].Kind)
	require.Equal(t, domain.MemoryKindObservation, stored[1].Kind)
	require.Len(t, repo.stored, 3)
	require.GreaterOrEqual(t, embedder.batchCalls, 1)
}

func TestStoreBatchWithParserUsesSingleBatchEmbedAcrossTurns(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	embedder := &countingBatchEmbedder{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedder,
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    true,
			MaxFacts:        5,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{Content: "Alice likes tea.", Kind: domain.MemoryKindObservation},
				{Content: "Alice moved to Austin.", Kind: domain.MemoryKindEvent},
			},
		}),
	)

	stored, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "Alice: I like tea.", Kind: domain.MemoryKindRawTurn},
		{TenantID: "tenant_1", Content: "Alice: I moved to Austin.", Kind: domain.MemoryKindRawTurn},
	})
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.Equal(t, 1, embedder.batchCalls)
	require.Equal(t, 4, embedder.lastBatchLen)
	require.Equal(t, 0, embedder.embedCalls)

	// Facts dedupe across turns while both raw turns are preserved.
	require.Len(t, repo.stored, 4)
	require.Len(t, vector.upserted, 4)
	require.Equal(t, 1, vector.batchCalls)
}

func TestStoreBatchWithParserSkipsRepoDedupeForSmallTenant(t *testing.T) {
	repo := &countingSearchRepoStub{}
	vector := &structuredVectorStub{}
	embedder := &countingBatchEmbedder{}
	svc := NewService(
		repo,
		tenantRepoStub{
			existsByID:      map[string]bool{"tenant_1": true},
			memoryCountByID: map[string]int64{"tenant_1": 0},
		},
		vector,
		embedder,
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    true,
			MaxFacts:        5,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{Content: "Alice likes tea.", Kind: domain.MemoryKindObservation},
				{Content: "Alice moved to Austin.", Kind: domain.MemoryKindEvent},
			},
		}),
	)

	_, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "turn one", Kind: domain.MemoryKindRawTurn},
		{TenantID: "tenant_1", Content: "turn two", Kind: domain.MemoryKindRawTurn},
	})
	require.NoError(t, err)
	require.Equal(t, 0, repo.searchCalls)
}

func TestStoreBatchWithParserRunsRepoDedupeForLargeTenant(t *testing.T) {
	repo := &countingSearchRepoStub{}
	vector := &structuredVectorStub{}
	embedder := &countingBatchEmbedder{}
	svc := NewService(
		repo,
		tenantRepoStub{
			existsByID:      map[string]bool{"tenant_1": true},
			memoryCountByID: map[string]int64{"tenant_1": parserBatchRepoDedupeMinTenantRows},
		},
		vector,
		embedder,
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    true,
			MaxFacts:        5,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{Content: "Alice likes tea.", Kind: domain.MemoryKindObservation},
				{Content: "Alice moved to Austin.", Kind: domain.MemoryKindEvent},
			},
		}),
	)

	_, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "turn one", Kind: domain.MemoryKindRawTurn},
		{TenantID: "tenant_1", Content: "turn two", Kind: domain.MemoryKindRawTurn},
	})
	require.NoError(t, err)
	require.Greater(t, repo.searchCalls, 0)
}

func TestStoreWithParserInjectsNormalizedTimeAnchorIntoParsedFacts(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content: "Caroline attended LGBTQ support group.",
					Kind:    domain.MemoryKindObservation,
				},
			},
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:1:56 pm on 8 May, 2023] Caroline: I went to a support group yesterday.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindObservation, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Contains(t, repo.stored[0].Content, "Caroline attended LGBTQ support group.")
	// Date is prepended as a natural-language phrase so FULL_DATE_RE can extract it.
	require.Contains(t, repo.stored[0].Content, "On 8 May 2023,")
}

func TestStoreWithHeuristicParserCanonicalizesAnnotatedTurnFacts(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(NewHeuristicInfoParser()),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:1:56 pm on 8 May, 2023] Caroline: I went to a support group yesterday and it was powerful.",
	})
	require.NoError(t, err)
	require.NotEmpty(t, stored.Content)
	require.Len(t, repo.stored, 2)
	for _, memory := range repo.stored {
		require.Contains(t, memory.Content, "On 8 May 2023,")
		require.NotContains(t, memory.Content, "1:56 pm on 8 May, 2023")
		require.NotContains(t, memory.Content, "Caroline:")
	}
	require.Contains(t, repo.stored[0].Content, "Caroline went to a support group yesterday and it was powerful.")
}

func TestStoreWithHeuristicParserSkipsLowSignalAnnotatedObservation(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    true,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(NewHeuristicInfoParser()),
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:9:55 am on 22 October, 2023] Melanie: Absolutely.",
	})
	require.NoError(t, err)
	require.Len(t, repo.stored, 1)
	require.Equal(t, domain.MemoryKindRawTurn, repo.stored[0].Kind)
}

func TestStoreWithParserRejectsShortLowSignalFact(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content: "great",
					Kind:    domain.MemoryKindObservation,
				},
			},
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice: Great.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindRawTurn, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Equal(t, domain.MemoryKindRawTurn, repo.stored[0].Kind)
}

func TestStoreWithParserAllowsShortHighSignalFact(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content: "teacher",
					Kind:    domain.MemoryKindObservation,
				},
			},
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice: I am a teacher.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindObservation, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Equal(t, "teacher", repo.stored[0].Content)
}

func TestStoreBatchWithParserRejectsShortFactsInBatchPath(t *testing.T) {
	repo := &structuredRepoStub{}
	vector := &structuredVectorStub{}
	embedder := &countingBatchEmbedder{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedder,
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			StoreRawTurn:    false,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content: "great",
					Kind:    domain.MemoryKindObservation,
				},
			},
		}),
	)

	stored, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "Alice: Great.", Kind: domain.MemoryKindRawTurn},
		{TenantID: "tenant_1", Content: "Bob: Nice.", Kind: domain.MemoryKindRawTurn},
	})
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.Equal(t, domain.MemoryKindRawTurn, stored[0].Kind)
	require.Equal(t, domain.MemoryKindRawTurn, stored[1].Kind)
	require.Len(t, repo.stored, 2)
	require.Equal(t, 1, embedder.batchCalls)
}
