package memory

import (
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
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

func TestBuildAdaptiveSearchQueriesSkipsWhenConfidenceAndLexicalAreStrong(t *testing.T) {
	tuning := defaultRetrievalSearchTuningOptions()
	queries := buildAdaptiveSearchQueries(
		"Who met Jordan yesterday?",
		queryProfile{},
		queryPlan{Confidence: 0.91},
		[]lexicalCandidate{
			{
				Memory: domain.Memory{ID: "m1", Kind: domain.MemoryKindObservation, Content: "Alex met Jordan yesterday."},
				Score:  0.88,
			},
		},
		tuning,
	)
	require.Empty(t, queries)
}

func TestBuildAdaptiveSearchQueriesAddsVariantsWhenFirstPassIsWeak(t *testing.T) {
	tuning := defaultRetrievalSearchTuningOptions()
	tuning.AdaptiveQueryExpansionEnabled = true
	queries := buildAdaptiveSearchQueries(
		"Which group did Caroline join near Austin?",
		queryProfile{},
		queryPlan{Confidence: 0.45},
		[]lexicalCandidate{
			{
				Memory: domain.Memory{ID: "m1", Kind: domain.MemoryKindObservation, Content: "Caroline met friends in Austin."},
				Score:  0.31,
			},
		},
		tuning,
	)
	require.NotEmpty(t, queries)
	require.LessOrEqual(t, len(queries), tuning.AdaptiveQueryMaxExtraQueries)
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

func TestBuildPseudoRelevanceQueriesSkipsStrongFirstPass(t *testing.T) {
	queries := buildPseudoRelevanceQueries(
		"which group did caroline join",
		queryProfile{},
		[]lexicalCandidate{
			{
				Memory: domain.Memory{
					ID:      "m1",
					Kind:    domain.MemoryKindObservation,
					Content: "Caroline joined an LGBTQ support group yesterday.",
				},
				Score: 0.95,
			},
			{
				Memory: domain.Memory{
					ID:      "m2",
					Kind:    domain.MemoryKindObservation,
					Content: "Caroline talked with Melanie.",
				},
				Score: 0.40,
			},
			{
				Memory: domain.Memory{ID: "m3", Kind: domain.MemoryKindObservation, Content: "Other memory"},
				Score:  0.35,
			},
			{
				Memory: domain.Memory{ID: "m4", Kind: domain.MemoryKindObservation, Content: "Other memory"},
				Score:  0.34,
			},
			{
				Memory: domain.Memory{ID: "m5", Kind: domain.MemoryKindObservation, Content: "Other memory"},
				Score:  0.33,
			},
			{
				Memory: domain.Memory{ID: "m6", Kind: domain.MemoryKindObservation, Content: "Other memory"},
				Score:  0.32,
			},
		},
		1,
	)
	require.Empty(t, queries)
}

func TestBuildPseudoRelevanceQueriesBuildsExpansionWhenFirstPassIsWeak(t *testing.T) {
	queries := buildPseudoRelevanceQueries(
		"which group did caroline join",
		queryProfile{},
		[]lexicalCandidate{
			{
				Memory: domain.Memory{
					ID:      "m1",
					Kind:    domain.MemoryKindObservation,
					Content: "Caroline joined an LGBTQ support group near Austin.",
				},
				Score: 0.46,
			},
			{
				Memory: domain.Memory{
					ID:      "m2",
					Kind:    domain.MemoryKindObservation,
					Content: "The support group met at a community center in Austin.",
				},
				Score: 0.42,
			},
			{
				Memory: domain.Memory{
					ID:      "m3",
					Kind:    domain.MemoryKindObservation,
					Content: "Caroline said the support group was welcoming.",
				},
				Score: 0.40,
			},
			{
				Memory: domain.Memory{ID: "m4", Kind: domain.MemoryKindObservation, Content: "Extra evidence about support group."},
				Score:  0.39,
			},
			{
				Memory: domain.Memory{ID: "m5", Kind: domain.MemoryKindObservation, Content: "Another support group fact for Caroline."},
				Score:  0.38,
			},
			{
				Memory: domain.Memory{ID: "m6", Kind: domain.MemoryKindObservation, Content: "Community support group memory."},
				Score:  0.37,
			},
		},
		1,
	)
	require.Len(t, queries, 1)
	require.Contains(t, queries[0], "support")
}

func TestNormalizedRankingTokensDropsStopwords(t *testing.T) {
	tokens := normalizedRankingTokens("What is the plan for the hiking trip?")
	_, hasPlan := tokens["plan"]
	_, hasTrip := tokens["trip"]
	_, hasWhat := tokens["what"]
	_, hasThe := tokens["the"]
	require.True(t, hasPlan)
	require.True(t, hasTrip)
	require.False(t, hasWhat)
	require.False(t, hasThe)
}

func TestNormalizedRankingTokensStopwordFallback(t *testing.T) {
	tokens := normalizedRankingTokens("what is it")
	_, hasWhat := tokens["what"]
	_, hasIs := tokens["is"]
	_, hasIt := tokens["it"]
	require.True(t, hasWhat)
	require.True(t, hasIs)
	require.True(t, hasIt)
}

func TestOrderedBigramCoverage(t *testing.T) {
	strong := orderedBigramCoverage(
		"support group meeting",
		"She joined a support group meeting yesterday.",
	)
	weak := orderedBigramCoverage(
		"support group meeting",
		"She joined a local support session yesterday.",
	)
	require.Greater(t, strong, 0.95)
	require.Less(t, weak, 0.60)
}

func TestIDFCoverageRewardsRareTokenMatch(t *testing.T) {
	candidates := []scoredMemory{
		{Memory: domain.Memory{Content: "caroline support group meeting"}},
		{Memory: domain.Memory{Content: "caroline support group"}},
		{Memory: domain.Memory{Content: "caroline meeting"}},
	}
	idf := buildLocalIDFMap("caroline support group meeting", candidates)
	withRare := idfCoverageScore("caroline support group meeting", "caroline met at support group meeting", idf)
	withoutRare := idfCoverageScore("caroline support group meeting", "caroline support group", idf)
	require.Greater(t, withRare, withoutRare)
}
