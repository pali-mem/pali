package ollama

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{name: "plain number", input: "0.73", want: 0.73},
		{name: "wrapped text", input: "score: 0.21", want: 0.21},
		{name: "reasoning tags stripped", input: "<think>considering 2024...</think>\nAnswer: 0.62", want: 0.62},
		{name: "clamp high", input: "1.4", want: 1},
		{name: "clamp low", input: "-0.4", want: 0},
		{name: "no number", input: "n/a", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseScore(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.InDelta(t, tc.want, got, 1e-9)
		})
	}
}
