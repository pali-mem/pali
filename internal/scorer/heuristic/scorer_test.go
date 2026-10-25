package heuristic

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func score(t *testing.T, text string) float64 {
	t.Helper()
	v, err := NewScorer().Score(context.Background(), text)
	require.NoError(t, err)
	require.GreaterOrEqual(t, v, 0.0)
	require.LessOrEqual(t, v, 1.0)
	return v
}

func TestScoreEmptyAndNoise(t *testing.T) {
	require.Equal(t, 0.0, score(t, ""))
	require.Equal(t, 0.0, score(t, "   "))
	require.Less(t, score(t, "the the the the the"), 0.15)
	require.Less(t, score(t, "ok"), 0.15)
}

func TestScoreInformativeFactOutranksChatter(t *testing.T) {
	fact := score(t, "Caroline moved to Boston in 2023 and works as a nurse at Mass General Hospital")
	longFact := score(t, "Melanie went to the Bali Arts Festival on 8 May 2023 with her husband and two children and loved the dance performances")
	for _, chatter := range []string{"ok", "xyzzy", "wow", "sounds good", "the the the the the"} {
		require.Greater(t, fact, score(t, chatter), "fact should outrank %q", chatter)
		require.Greater(t, longFact, score(t, chatter), "long fact should outrank %q", chatter)
	}
	require.GreaterOrEqual(t, longFact, fact*0.8, "longer informative fact must not collapse below a shorter one")
}

func TestScoreKeySignalBoosts(t *testing.T) {
	base := score(t, "Caroline drinks coffee in the morning")
	boosted := score(t, "Caroline always prefers coffee in the morning, remember that")
	require.Greater(t, boosted, base)
}

func TestBatchScoreMatchesScore(t *testing.T) {
	texts := []string{"", "ok", "Caroline moved to Boston in 2023"}
	batch, err := NewScorer().BatchScore(context.Background(), texts)
	require.NoError(t, err)
	require.Len(t, batch, len(texts))
	for i, text := range texts {
		require.Equal(t, score(t, text), batch[i])
	}
}
