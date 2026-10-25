package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A single-sentence utterance must not be emitted twice (whole-utterance form
// plus per-sentence form) as near-identical observations.
func TestDeriveTurnObservationsSingleSentenceEmitsOnce(t *testing.T) {
	turn := parsedTurn{Speaker: "Melanie", Time: "3:04 pm on 8 May, 2023", Utterance: "I went to the Bali Arts Festival last weekend."}
	out := deriveTurnObservations(turn, 4)
	require.Len(t, out, 1, "%v", out)
}

func TestDeriveTurnObservationsMultiSentenceKeepsWholeAndPerSentence(t *testing.T) {
	// The whole-utterance form carries cross-sentence context that multi-hop
	// questions need, so it is kept alongside the per-sentence forms.
	turn := parsedTurn{Speaker: "Melanie", Utterance: "I went to the Bali Arts Festival last weekend. My sister got married in Napa."}
	out := deriveTurnObservations(turn, 4)
	require.Len(t, out, 3, "%v", out)
}

// Pronoun subjects are never resolved entities, regardless of source shape.
func TestHasResolvedFactSubjectRejectsPronouns(t *testing.T) {
	require.False(t, hasResolvedFactSubject("plain source", "She works as a nurse", "She"))
	require.False(t, hasResolvedFactSubject("plain source", "It was a great festival in Napa", ""))
	require.True(t, hasResolvedFactSubject("plain source", "Caroline works as a nurse", "Caroline"))
}

// Length alone must not bypass the informativeness filter.
func TestShouldStoreParsedFactContentRejectsLongQuestions(t *testing.T) {
	require.False(t, shouldStoreParsedFactContent("What did you think about the festival last weekend?"))
	require.False(t, shouldStoreParsedFactContent("Do you remember when we went to the Bali Arts Festival"))
	require.True(t, shouldStoreParsedFactContent("Melanie went to the Bali Arts Festival on 8 May 2023"))
}
