package memory

import (
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestCompareScoreDescThenID(t *testing.T) {
	require.Equal(t, -1, compareScoreDescThenID(0.9, 0.1, "b", "a"))
	require.Equal(t, 1, compareScoreDescThenID(0.1, 0.9, "a", "b"))
	require.Equal(t, -1, compareScoreDescThenID(0.5, 0.5, "a", "b"))
	require.Equal(t, 1, compareScoreDescThenID(0.5, 0.5, "b", "a"))
	require.Equal(t, 0, compareScoreDescThenID(0.5, 0.5, "a", "a"))
}

func TestPassesFilters(t *testing.T) {
	memory := domain.Memory{Kind: domain.MemoryKindEvent, Tier: domain.MemoryTierSemantic}
	require.True(t, passesFilters(memory, nil, nil))
	require.True(t, passesFilters(memory, map[domain.MemoryKind]struct{}{domain.MemoryKindEvent: {}}, map[domain.MemoryTier]struct{}{domain.MemoryTierSemantic: {}}))
	require.False(t, passesFilters(memory, map[domain.MemoryKind]struct{}{domain.MemoryKindRawTurn: {}}, nil))
	require.False(t, passesFilters(memory, nil, map[domain.MemoryTier]struct{}{domain.MemoryTierWorking: {}}))
}

func TestEntityLookupLimit(t *testing.T) {
	for topK, want := range map[int]int{0: 8, 50: 8, 89: 8, 120: 12, 240: 24, 1000: 24} {
		require.Equal(t, want, entityLookupLimit(topK), "topK=%d", topK)
	}
}
