package memory

import (
	"context"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPrepareParsedFactsForStoreUsesAnnotatedSpeakerForFirstPersonFact(t *testing.T) {
	source := "[TIME: 8 May, 2023] Melanie: I use TypeScript for backend services."
	prepared := prepareParsedFactsForStore(source, []ParsedFact{
		{
			Content:  "I use TypeScript for backend services.",
			Kind:     domain.MemoryKindObservation,
			Relation: "tool",
			Value:    "TypeScript",
		},
	}, false)
	require.Len(t, prepared, 1)
	require.Equal(t, "Melanie", prepared[0].Entity)
}

func TestPrepareParsedFactsForStoreDoesNotInventUserEntity(t *testing.T) {
	prepared := prepareParsedFactsForStore("I use TypeScript for backend services.", []ParsedFact{
		{
			Content:  "I use TypeScript for backend services.",
			Kind:     domain.MemoryKindObservation,
			Relation: "tool",
			Value:    "TypeScript",
		},
	}, false)
	require.Len(t, prepared, 1)
	require.Equal(t, "", prepared[0].Entity)

	_, ok := buildEntityFactRecord(domain.Memory{ID: "m1", TenantID: "t"}, prepared[0])
	require.False(t, ok)
}

func TestStoreWithParserSkipsEntityFactButStoresMemoryWhenSpeakerUnknown(t *testing.T) {
	repo := &structuredRepoStub{}
	entityRepo := &parserEntityFactRepoStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		&structuredVectorStub{},
		&countingBatchEmbedder{},
		scorerStub{},
		ParserOptions{Enabled: true, StoreRawTurn: false, MaxFacts: 4},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content:  "I enjoy camping in the mountains.",
					Kind:     domain.MemoryKindObservation,
					Relation: "activity",
					Value:    "camping in the mountains",
				},
			},
		}),
		WithEntityFactRepository(entityRepo),
	)

	_, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "I enjoy camping in the mountains.", Kind: domain.MemoryKindRawTurn},
	})
	require.NoError(t, err)
	require.NotEmpty(t, repo.stored)
	require.Empty(t, entityRepo.stored)
}

func TestStoreWithParserEntityFactUsesAnnotatedSpeaker(t *testing.T) {
	repo := &structuredRepoStub{}
	entityRepo := &parserEntityFactRepoStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		&structuredVectorStub{},
		&countingBatchEmbedder{},
		scorerStub{},
		ParserOptions{Enabled: true, StoreRawTurn: false, MaxFacts: 4},
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{
					Content:  "I enjoy camping in the mountains.",
					Kind:     domain.MemoryKindObservation,
					Relation: "activity",
					Value:    "camping in the mountains",
				},
			},
		}),
		WithEntityFactRepository(entityRepo),
	)

	_, err := svc.StoreBatch(context.Background(), []StoreInput{
		{TenantID: "tenant_1", Content: "[TIME: 8 May, 2023] Melanie: I enjoy camping in the mountains.", Kind: domain.MemoryKindRawTurn},
	})
	require.NoError(t, err)
	require.Len(t, entityRepo.stored, 1)
	require.Equal(t, "melanie", entityRepo.stored[0].Entity)
}
