package memory

import (
	"fmt"
	"testing"
	"time"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

// Routed (temporal/person/multi-hop) queries must preserve the relevance
// ordering among same-kind candidates instead of clamping the head to 1.0.
func TestRankMemoriesRoutedQueryPreservesRelevanceOrder(t *testing.T) {
	query := "when did the trip to paris happen"
	profile := classifyQuery(query)
	require.True(t, profile.Temporal)
	plan := buildQueryPlan(query, profile)

	now := time.Now().UTC()
	const n = 20
	memories := make([]domain.Memory, 0, n)
	signals := make(map[string]candidateSignal, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("m%02d", i)
		memories = append(memories, domain.Memory{
			ID:             id,
			Kind:           domain.MemoryKindEvent,
			Content:        fmt.Sprintf("Event %d about the trip to Paris", i),
			Importance:     0.5,
			CreatedAt:      now,
			UpdatedAt:      now,
			LastAccessedAt: now,
		})
		dense := 1.0 - float64(i)*0.04
		signals[id] = candidateSignal{
			DenseScore:   dense,
			DenseRank:    i + 1,
			LexicalScore: dense,
			LexicalRank:  i + 1,
			RRFScore:     dense,
		}
	}

	scored := rankMemories(memories, signals, n, query, profile, plan, defaultRankingOptions(), defaultRetrievalBehaviorOptions())
	require.Len(t, scored, n)
	for i := 1; i < n; i++ {
		require.Greater(t, scored[i-1].Score, scored[i].Score,
			"candidate %s (%.4f) should outscore %s (%.4f)", scored[i-1].Memory.ID, scored[i-1].Score, scored[i].Memory.ID, scored[i].Score)
	}
}

// A candidate with zero normalized relevance must not be promoted to the top.
func TestRankMemoriesRoutedQueryDoesNotPromoteZeroRelevance(t *testing.T) {
	query := "when did the trip to paris happen"
	profile := classifyQuery(query)
	require.True(t, profile.Temporal)
	plan := buildQueryPlan(query, profile)

	now := time.Now().UTC()
	memories := []domain.Memory{
		{ID: "good", Kind: domain.MemoryKindEvent, Content: "The trip to Paris happened in May 2023", Importance: 0.5, CreatedAt: now, UpdatedAt: now, LastAccessedAt: now},
		{ID: "decoy", Kind: domain.MemoryKindEvent, Content: "unrelated zzz", Importance: 0.5, CreatedAt: now, UpdatedAt: now, LastAccessedAt: now},
	}
	signals := map[string]candidateSignal{
		"good":  {DenseScore: 0.9, DenseRank: 1, LexicalScore: 0.9, LexicalRank: 1, RRFScore: 0.9},
		"decoy": {DenseScore: 0.1, DenseRank: 2, LexicalScore: 0.0, LexicalRank: 2, RRFScore: 0.1},
	}

	scored := rankMemories(memories, signals, 2, query, profile, plan, defaultRankingOptions(), defaultRetrievalBehaviorOptions())
	byID := map[string]float64{}
	for _, s := range scored {
		byID[s.Memory.ID] = s.Score
	}
	require.Greater(t, byID["good"], byID["decoy"])
}

func rerankTestCandidates() []scoredMemory {
	return []scoredMemory{
		{Memory: domain.Memory{ID: "generic", Content: "General life update and weekend plans."}, Score: 0.9},
		{Memory: domain.Memory{ID: "support_group", Content: "Caroline joined an LGBTQ support group yesterday.", QueryViewText: "caroline joined support group"}, Score: 0.5},
	}
}

func TestRerankScoredCandidatesHonoursEnabled(t *testing.T) {
	query := "which group did caroline join"

	enabled := NewService(nil, nil, nil, nil, nil, RerankOptions{Enabled: true, Window: 50, Blend: 1})
	reranked := enabled.rerankScoredCandidates(rerankTestCandidates(), query, 5)
	require.Equal(t, "support_group", reranked[0].Memory.ID)

	disabled := NewService(nil, nil, nil, nil, nil, RerankOptions{Enabled: false, Window: 50, Blend: 1})
	require.Equal(t, rerankTestCandidates(), disabled.rerankScoredCandidates(rerankTestCandidates(), query, 5))
}

func TestRerankScoredCandidatesZeroBlendKeepsFirstStageScores(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, RerankOptions{Enabled: true, Window: 50, Blend: 0})
	require.Equal(t, rerankTestCandidates(), svc.rerankScoredCandidates(rerankTestCandidates(), "which group did caroline join", 5))
}

func TestNormalizeRerankOptionsKeepsExplicitValues(t *testing.T) {
	def := defaultRerankOptions()
	require.True(t, def.Enabled, "reranking stays on by default")
	require.Equal(t, def, NewService(nil, nil, nil, nil, nil).rerank)

	require.Equal(t, RerankOptions{Enabled: false, Window: 7, Blend: 0}, normalizeRerankOptions(RerankOptions{Enabled: false, Window: 7, Blend: 0}))
	require.Equal(t, RerankOptions{Enabled: true, Window: def.Window, Blend: 0.25}, normalizeRerankOptions(RerankOptions{Enabled: true, Blend: 0.25}))
	require.Equal(t, 0.0, normalizeRerankOptions(RerankOptions{Blend: -0.5}).Blend)
	require.Equal(t, 1.0, normalizeRerankOptions(RerankOptions{Blend: 1.5}).Blend)
}
