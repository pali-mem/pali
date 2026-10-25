package memory

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTimeTagPatternDoesNotMatchOrdinaryWords(t *testing.T) {
	for _, word := range []string{
		"married", "marriage", "mayor", "maybe", "junior", "julia",
		"decide", "novel", "augment", "apricot",
		"i may go later", "the 1234 code",
	} {
		require.False(t, timeTagPattern.MatchString(word), "should not match %q", word)
	}
}

func TestTimeTagPatternMatchesRealDates(t *testing.T) {
	for _, text := range []string{
		"8 may, 2023", "8 may 2023", "may 8", "in january", "on 2 jan 2006",
		"october 2021", "sept 3", "in 2023", "[time: 3:04 pm on 8 May, 2023]",
	} {
		require.True(t, timeTagPattern.MatchString(text), "should match %q", text)
	}
}

func TestHasAbsoluteOrAnchoredTimeRejectsMarriedWithoutAnchor(t *testing.T) {
	require.False(t, hasAbsoluteOrAnchoredTime("plain source", "Melanie is so excited, my sister just got married in Napa."))
}
