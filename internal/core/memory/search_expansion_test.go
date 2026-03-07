package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/pali-mem/pali/internal/domain"
)

func TestBuildSearchQueriesAddsIntentAwareRewriteForWhyQuery(t *testing.T) {
	queries := buildSearchQueries("Why did Melanie start running?", queryProfile{})
	joined := ""
	for _, query := range queries {
		joined += " " + query
	}
	require.Contains(t, joined, "motivation")
	require.Contains(t, joined, "because")
}

func TestBuildSearchQueriesAddsIntentAwareRewriteForSymbolQuery(t *testing.T) {
	queries := buildSearchQueries("What does Caroline's necklace symbolize?", queryProfile{})
	joined := ""
	for _, query := range queries {
		joined += " " + query
	}
	require.Contains(t, joined, "reminder")
	require.Contains(t, joined, "represents")
}

func TestBuildIterativeMultiHopQueriesSkipsStrongFirstHop(t *testing.T) {
	queries := buildIterativeMultiHopQueries(
		"who met Jordan before moving to Austin?",
		[]lexicalCandidate{
			{
				Memory: domain.Memory{
					ID:      "m1",
					Kind:    domain.MemoryKindObservation,
					Content: "Alex met Jordan before moving to Austin.",
				},
				Score: 0.91,
			},
		},
		1,
	)
	require.Empty(t, queries)
}

func TestBuildIterativeMultiHopQueriesRequiresNovelEvidenceTokens(t *testing.T) {
	queries := buildIterativeMultiHopQueries(
		"who met Jordan before moving to Austin?",
		[]lexicalCandidate{
			{
				Memory: domain.Memory{
					ID:      "m1",
					Kind:    domain.MemoryKindObservation,
					Content: "Jordan moved to Austin before.",
				},
				Score: 0.35,
			},
		},
		1,
	)
	require.Empty(t, queries)
}

func TestBuildIterativeMultiHopQueriesProducesRefinedQueryForWeakFirstHop(t *testing.T) {
	queries := buildIterativeMultiHopQueries(
		"who met Jordan before moving to Austin?",
		[]lexicalCandidate{
			{
				Memory: domain.Memory{
					ID:      "m1",
					Kind:    domain.MemoryKindObservation,
					Content: "Alex met Jordan in Seattle before moving elsewhere.",
				},
				Score: 0.28,
			},
		},
		1,
	)
	require.Len(t, queries, 1)
}
