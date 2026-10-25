package memory

import (
	"context"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func maxFactsTestService(opts ParserOptions) *Service {
	return NewService(
		&structuredRepoStub{},
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		&structuredVectorStub{},
		embedderStub{},
		scorerStub{},
		opts,
		WithInfoParser(parserStub{
			facts: []ParsedFact{
				{Content: "Alice lives in Austin.", Kind: domain.MemoryKindObservation},
				{Content: "Alice works as a nurse.", Kind: domain.MemoryKindObservation},
				{Content: "Alice enjoys playing the violin.", Kind: domain.MemoryKindObservation},
				{Content: "Alice has a brother named Daniel.", Kind: domain.MemoryKindObservation},
			},
		}),
	)
}

func TestParserMaxFactsCap(t *testing.T) {
	cases := []struct {
		name string
		opts ParserOptions
		want int
	}{
		{"default with raw turn keeps one", ParserOptions{Enabled: true, StoreRawTurn: true}, 1},
		{"default without raw turn keeps two", ParserOptions{Enabled: true, StoreRawTurn: false}, 2},
		{"explicit 1", ParserOptions{Enabled: true, StoreRawTurn: true, MaxFacts: 1}, 1},
		{"explicit 3 with raw turn", ParserOptions{Enabled: true, StoreRawTurn: true, MaxFacts: 3}, 3},
		{"explicit 4 without raw turn", ParserOptions{Enabled: true, StoreRawTurn: false, MaxFacts: 4}, 4},
		{"explicit above candidates", ParserOptions{Enabled: true, StoreRawTurn: true, MaxFacts: 9}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := maxFactsTestService(tc.opts)
			result, err := svc.parseFactsWithFallback(context.Background(), "Alice: I live in Austin, work as a nurse, play the violin and have a brother named Daniel.", 1)
			require.NoError(t, err)
			require.Len(t, result.Facts, tc.want)
		})
	}
}
