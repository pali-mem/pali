package memory

import (
	"slices"
	"testing"
	"time"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

// Equal BM25 scores must be ordered newest UpdatedAt first. The IDs are chosen
// so that ID order disagrees with recency: dropping the UpdatedAt tie-break
// would put "mem_a_older" first and fail this test.
func TestCompareLexicalCandidatesBreaksScoreTiesByUpdatedAt(t *testing.T) {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	older := lexicalCandidate{Memory: domain.Memory{ID: "mem_a_older", UpdatedAt: base}, Score: 0.5}
	newer := lexicalCandidate{Memory: domain.Memory{ID: "mem_b_newer", UpdatedAt: base.Add(time.Hour)}, Score: 0.5}
	stronger := lexicalCandidate{Memory: domain.Memory{ID: "mem_z_stronger", UpdatedAt: base.Add(-time.Hour)}, Score: 0.9}
	sameTimeLowID := lexicalCandidate{Memory: domain.Memory{ID: "mem_c_same", UpdatedAt: base}, Score: 0.2}
	sameTimeHighID := lexicalCandidate{Memory: domain.Memory{ID: "mem_d_same", UpdatedAt: base}, Score: 0.2}

	require.Positive(t, compareLexicalCandidates(older, newer))
	require.Negative(t, compareLexicalCandidates(newer, older))
	require.Negative(t, compareLexicalCandidates(stronger, newer), "score outranks recency")
	require.Negative(t, compareLexicalCandidates(sameTimeLowID, sameTimeHighID), "ID breaks remaining ties")

	for _, input := range [][]lexicalCandidate{
		{older, sameTimeHighID, newer, stronger, sameTimeLowID},
		{sameTimeLowID, stronger, newer, sameTimeHighID, older},
	} {
		sorted := append([]lexicalCandidate{}, input...)
		slices.SortFunc(sorted, compareLexicalCandidates)
		ids := make([]string, 0, len(sorted))
		for _, c := range sorted {
			ids = append(ids, c.Memory.ID)
		}
		require.Equal(t, []string{"mem_z_stronger", "mem_b_newer", "mem_a_older", "mem_c_same", "mem_d_same"}, ids)
	}
}
