package memory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
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

type parserFuncStub func(context.Context, string, int) ([]ParsedFact, error)

func (f parserFuncStub) Parse(ctx context.Context, content string, maxFacts int) ([]ParsedFact, error) {
	return f(ctx, content, maxFacts)
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

func TestStoreWithParserPersistsSourceGroundedIdentity(t *testing.T) {
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
			Provider:        "ollama",
			Model:           "qwen3:4b",
			StoreRawTurn:    true,
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
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Melanie: I enjoy camping.",
	})
	require.NoError(t, err)
	require.Len(t, repo.stored, 2)

	rawTurn := repo.stored[0]
	fact := repo.stored[1]
	require.NotEmpty(t, rawTurn.CanonicalKey)
	require.NotEmpty(t, rawTurn.SourceTurnHash)
	require.Equal(t, -1, rawTurn.SourceFactIndex)
	require.Equal(t, rawTurnExtractorName, rawTurn.Extractor)
	require.Equal(t, rawTurnExtractorVersion, rawTurn.ExtractorVersion)

	require.NotEmpty(t, fact.CanonicalKey)
	require.Equal(t, rawTurn.SourceTurnHash, fact.SourceTurnHash)
	require.Equal(t, 0, fact.SourceFactIndex)
	require.Equal(t, "ollama", fact.Extractor)
	require.Equal(t, "qwen3:4b", fact.ExtractorVersion)
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

func TestStoreBatchWithParserRetainsAnswerMetadata(t *testing.T) {
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
			Enabled:                    true,
			StoreRawTurn:               false,
			MaxFacts:                   4,
			DedupeThreshold:            0.88,
			UpdateThreshold:            0.94,
			AnswerSpanRetentionEnabled: true,
		},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content:  "Caroline attended a LGBTQ support group.",
					Kind:     domain.MemoryKindEvent,
					Entity:   "Caroline",
					Relation: "event",
					Value:    "LGBTQ support group",
				},
			},
		}),
	)

	stored, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "[time:1:56 pm on 8 May, 2023] Caroline: I attended a LGBTQ support group yesterday.", Kind: domain.MemoryKindRawTurn},
		{TenantID: "tenant_1", Content: "[time:9:10 am on 9 May, 2023] Caroline: The support group was inspiring.", Kind: domain.MemoryKindRawTurn},
	})
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.Len(t, repo.stored, 1)
	require.Equal(t, "entity", repo.stored[0].AnswerMetadata.AnswerKind)
	require.Equal(t, "LGBTQ support group", repo.stored[0].AnswerMetadata.SurfaceSpan)
	require.Equal(t, "8 May 2023", repo.stored[0].AnswerMetadata.TemporalAnchor)
	require.NotEmpty(t, repo.stored[0].AnswerMetadata.SourceSentence)
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

func TestStoreWithParserUsesCanonicalLookupBeforeSecondLexicalDedupe(t *testing.T) {
	repo := &countingSearchRepoStub{}
	vector := &structuredVectorStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
		ParserOptions{
			Enabled:         true,
			Provider:        "ollama",
			Model:           "qwen3:4b",
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
	require.Equal(t, 0, repo.searchCalls)
	require.NotEmpty(t, repo.stored[0].CanonicalKey)
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

func TestStoreWithParserDedupesByRelationTupleWithoutLexicalSearch(t *testing.T) {
	repo := &countingSearchRepoStub{}
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
			MaxFacts:        5,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserFuncStub(func(_ context.Context, content string, maxFacts int) ([]ParsedFact, error) {
			if strings.Contains(content, "relocated") {
				return []ParsedFact{{
					Content:  "Alice relocated to Austin.",
					Kind:     domain.MemoryKindEvent,
					Entity:   "Alice",
					Relation: "place",
					Value:    "Austin",
				}}, nil
			}
			return []ParsedFact{{
				Content:  "Alice moved to Austin.",
				Kind:     domain.MemoryKindEvent,
				Entity:   "Alice",
				Relation: "place",
				Value:    "Austin",
			}}, nil
		})),
		WithEntityFactRepository(entityRepo),
	)

	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice: I moved to Austin.",
	})
	require.NoError(t, err)
	_, err = svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "Alice: I relocated to Austin.",
	})
	require.NoError(t, err)

	require.Len(t, repo.stored, 1)
	require.Equal(t, 0, repo.searchCalls)
}

func TestStoreBatchWithParserDedupesByRelationTupleWithinPendingBatch(t *testing.T) {
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
			StoreRawTurn:    true,
			MaxFacts:        5,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		WithInfoParser(parserFuncStub(func(_ context.Context, content string, maxFacts int) ([]ParsedFact, error) {
			if strings.Contains(content, "relocated") {
				return []ParsedFact{{
					Content:  "Alice relocated to Austin.",
					Kind:     domain.MemoryKindEvent,
					Entity:   "Alice",
					Relation: "place",
					Value:    "Austin",
				}}, nil
			}
			return []ParsedFact{{
				Content:  "Alice moved to Austin.",
				Kind:     domain.MemoryKindEvent,
				Entity:   "Alice",
				Relation: "place",
				Value:    "Austin",
			}}, nil
		})),
		WithEntityFactRepository(entityRepo),
	)

	_, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "Alice: I moved to Austin.", Kind: domain.MemoryKindRawTurn},
		{TenantID: "tenant_1", Content: "Alice: I relocated to Austin.", Kind: domain.MemoryKindRawTurn},
	})
	require.NoError(t, err)
	require.Len(t, repo.stored, 3)
	require.Equal(t, 2, countKind(repo.stored, domain.MemoryKindRawTurn))
	require.Equal(t, 1, countKind(repo.stored, domain.MemoryKindEvent))
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
	require.Len(t, repo.stored, 1)
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

func TestStoreWithParserRejectsVagueAnchoredFact(t *testing.T) {
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
					Content: "Melanie will do that.",
					Kind:    domain.MemoryKindObservation,
				},
			},
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:1:12 pm on 13 Oct, 2023] Melanie: I'll do that.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindRawTurn, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Equal(t, domain.MemoryKindRawTurn, repo.stored[0].Kind)
}

func TestStoreWithParserRejectsBareEmotionFact(t *testing.T) {
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
					Content: "Caroline is so excited and thankful.",
					Kind:    domain.MemoryKindObservation,
				},
			},
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:1:56 pm on 22 Oct, 2023] Caroline: I'm so excited and thankful.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindRawTurn, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Equal(t, domain.MemoryKindRawTurn, repo.stored[0].Kind)
}

func TestStoreWithParserKeepsEmotionFactWithConcreteTarget(t *testing.T) {
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
					Content:  "Caroline is excited about the adoption process.",
					Kind:     domain.MemoryKindObservation,
					Entity:   "Caroline",
					Relation: "identity",
					Value:    "excited about the adoption process",
				},
			},
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:1:56 pm on 22 Oct, 2023] Caroline: I'm excited about the adoption process.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindObservation, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Equal(t, "On 22 Oct 2023, Caroline is excited about the adoption process.", repo.stored[0].Content)
}

func TestStoreWithParserRejectsSpeechReactionFact(t *testing.T) {
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
					Content: "Caroline said that that sounds like fun.",
					Kind:    domain.MemoryKindObservation,
				},
			},
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:1:56 pm on 13 Sep, 2023] Caroline: That sounds like fun.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindRawTurn, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Equal(t, domain.MemoryKindRawTurn, repo.stored[0].Kind)
}

func TestStoreWithParserRejectsVisualCommentaryFact(t *testing.T) {
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
					Content: "Melanie said that love the red and blue.",
					Kind:    domain.MemoryKindObservation,
				},
			},
		}),
	)

	stored, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_1",
		Content:  "[time:1:56 pm on 13 Sep, 2023] Melanie: Love the red and blue.",
	})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindRawTurn, stored.Kind)
	require.Len(t, repo.stored, 1)
	require.Equal(t, domain.MemoryKindRawTurn, repo.stored[0].Kind)
}

func TestStoreWithParserAllowsSelfContainedHighSignalFact(t *testing.T) {
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
					Content: "Alice is a teacher.",
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
	require.Equal(t, "Alice is a teacher.", repo.stored[0].Content)
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

func TestPrepareParsedFactsForStoreSplitsCompoundFactWithRepeatedSubject(t *testing.T) {
	source := "Alice likes tea and Alice moved to Austin."
	prepared := prepareParsedFactsForStore(source, []ParsedFact{
		{
			Content: source,
			Kind:    domain.MemoryKindObservation,
		},
	}, false)
	require.Len(t, prepared, 2)
	require.Equal(t, "Alice likes tea", prepared[0].Content)
	require.Equal(t, "Alice moved to Austin.", prepared[1].Content)
}

func TestPrepareParsedFactsForStoreKeepsCompoundFactWithImplicitSubject(t *testing.T) {
	source := "Alice is vegetarian and avoids dairy."
	prepared := prepareParsedFactsForStore(source, []ParsedFact{
		{
			Content: source,
			Kind:    domain.MemoryKindObservation,
		},
	}, false)
	require.Len(t, prepared, 1)
	require.Equal(t, "Alice is vegetarian and avoids dairy.", prepared[0].Content)
}

func TestPrepareParsedFactsForStoreBackfillsUserEntityWhenRelationPresent(t *testing.T) {
	source := "I use TypeScript."
	prepared := prepareParsedFactsForStore(source, []ParsedFact{
		{
			Content:  source,
			Kind:     domain.MemoryKindObservation,
			Relation: "tool",
			Value:    "TypeScript",
		},
	}, false)
	require.Len(t, prepared, 1)
	require.Equal(t, "user", strings.ToLower(prepared[0].Entity))
	require.Equal(t, "tool", prepared[0].Relation)
	require.Equal(t, "TypeScript", prepared[0].Value)
}

func TestPrepareParsedFactsForStoreRetainsAnswerMetadataWhenEnabled(t *testing.T) {
	source := `[time:1:56 pm on 8 May, 2023] Caroline: I went to the "LGBTQ support group" yesterday.`
	prepared := prepareParsedFactsForStore(source, []ParsedFact{
		{
			Content: `Caroline went to the "LGBTQ support group" yesterday.`,
			Kind:    domain.MemoryKindEvent,
		},
	}, true)
	require.Len(t, prepared, 1)
	require.Equal(t, "quote", prepared[0].AnswerMetadata.AnswerKind)
	require.Equal(t, "LGBTQ support group", prepared[0].AnswerMetadata.SurfaceSpan)
	require.Equal(t, "8 May 2023", prepared[0].AnswerMetadata.TemporalAnchor)
	require.Equal(t, "yesterday", strings.ToLower(prepared[0].AnswerMetadata.RelativeTimePhrase))
}

func TestPrepareParsedFactsForStoreRejectsParserScaffoldTimestampFact(t *testing.T) {
	source := `[time:11:41 am on 6 November, 2023] Tim: That is one of my favorite fantasy shows.`
	prepared := prepareParsedFactsForStore(source, []ParsedFact{
		{
			Content: `Tim made this statement said that 11:41 am on 6 November 2023.`,
			Kind:    domain.MemoryKindEvent,
			Entity:  "Tim",
			Value:   "11:41 am on 6 November 2023",
		},
	}, true)
	require.Empty(t, prepared)
}

func TestPrepareParsedFactsForStoreDropsGenericQueryViewLines(t *testing.T) {
	source := "Nate is providing custom controller decorations for everyone."
	prepared := prepareParsedFactsForStore(source, []ParsedFact{
		{
			Content:  source,
			Kind:     domain.MemoryKindEvent,
			Entity:   "Nate",
			Relation: "event",
			Value:    "custom controller decorations for everyone",
		},
	}, false)
	require.Len(t, prepared, 1)
	require.Equal(
		t,
		"when did Nate custom controller decorations for everyone\nwhat event did Nate attend custom controller decorations for everyone\nNate custom controller decorations for everyone",
		prepared[0].QueryViewText,
	)
}
