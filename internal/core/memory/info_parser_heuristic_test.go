package memory

import "testing"

import "github.com/stretchr/testify/require"

func TestIsInformativeFact_AcceptsHighSignalShortFacts(t *testing.T) {
	cases := []string{
		"single",
		"2022",
		"4 years",
		"transgender woman",
		"bisexual",
		"engineer",
	}
	for _, tc := range cases {
		require.True(t, isInformativeFact(tc), tc)
	}
}

func TestIsInformativeFact_RejectsLowSignalShortFacts(t *testing.T) {
	cases := []string{
		"ok",
		"yes",
		"sure",
		"hmm",
		"oh",
		"thanks",
	}
	for _, tc := range cases {
		require.False(t, isInformativeFact(tc), tc)
	}
}
