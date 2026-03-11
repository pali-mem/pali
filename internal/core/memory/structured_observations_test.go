package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveObservations_AnnotatedTurn(t *testing.T) {
	content := "[sample:conv-26] [dialog:D1:3] [time:1:56 pm on 8 May, 2023] [speaker_a:Caroline] [speaker_b:Melanie] Caroline: I went to a support group yesterday and it was powerful."
	derived, err := deriveObservations(content, 3)
	require.NoError(t, err)
	require.NotEmpty(t, derived)
	require.Contains(t, derived[0], "Caroline")
	require.Contains(t, derived[0], "1:56 pm on 8 May, 2023")
	require.Contains(t, derived[0], "support group")
	for _, item := range derived {
		require.NotContains(t, item, "Conversation participants:")
	}
}

func TestDeriveEvent_AnnotatedTurn(t *testing.T) {
	content := "[time:1:56 pm on 8 May, 2023] Caroline: I went to a support group yesterday."
	event, ok := deriveEvent(content)
	require.True(t, ok)
	require.Contains(t, event, "Caroline")
	require.Contains(t, event, "1:56 pm on 8 May, 2023")
	require.Contains(t, event, "support group")
}

func TestDeriveObservations_SingleSentenceFallback(t *testing.T) {
	derived, err := deriveObservations("User likes concise replies.", 3)
	require.NoError(t, err)
	require.Empty(t, derived)
}

func TestSourceTimeAnchor_NormalizesAnnotatedTurnTime(t *testing.T) {
	content := "[time:1:56 pm on 8 May, 2023] Caroline: I went to a support group yesterday."
	anchor, ok := sourceTimeAnchor(content)
	require.True(t, ok)
	// Expect human-readable "D Mon YYYY" — directly matched by FULL_DATE_RE in eval.
	require.Equal(t, "8 May 2023", anchor)
}

func TestSourceTimeAnchor_UsesRawWhenUnparseable(t *testing.T) {
	content := "[time:yesterday evening] Caroline: I went to a support group yesterday."
	anchor, ok := sourceTimeAnchor(content)
	require.True(t, ok)
	require.Equal(t, "yesterday evening", anchor)
}

func TestCanonicalizeTurnStyleFact_RewritesParserObservation(t *testing.T) {
	source := "[time:1:56 pm on 8 May, 2023] Caroline: I went to a support group yesterday and it was powerful."
	fact := "Caroline said at 1:56 pm on 8 May, 2023: I went to a support group yesterday and it was powerful."
	require.Equal(
		t,
		"Caroline went to a support group yesterday and it was powerful.",
		canonicalizeTurnStyleFact(source, fact),
	)
}

func TestCanonicalizeTurnStyleFact_DropsLowSignalTurnEcho(t *testing.T) {
	source := "[time:9:55 am on 22 October, 2023] Melanie: Absolutely."
	fact := "[time:9:55 am on 22 October, 2023] Melanie: Absolutely."
	require.Empty(t, canonicalizeTurnStyleFact(source, fact))
}
