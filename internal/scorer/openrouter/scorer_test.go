package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

func TestParseScoreList(t *testing.T) {
	t.Parallel()

	scores, err := parseScoreList(`{"scores":[0.1,0.9,1.2,-1]}`, 4)
	require.NoError(t, err)
	require.Equal(t, []float64{0.1, 0.9, 1, 0}, scores)

	scores, err = parseScoreList("answer: [0.2, 0.3, 0.4]", 3)
	require.NoError(t, err)
	require.Equal(t, []float64{0.2, 0.3, 0.4}, scores)

	_, err = parseScoreList(`{"scores":[0.1]}`, 2)
	require.Error(t, err)
}

func TestBatchScoreChunkingAndOrder(t *testing.T) {
	origChunk := openRouterMaxBatchScores
	origParallel := openRouterMaxParallelScorings
	openRouterMaxBatchScores = 2
	openRouterMaxParallelScorings = 2
	defer func() {
		openRouterMaxBatchScores = origChunk
		openRouterMaxParallelScorings = origParallel
	}()

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/chat/completions", r.URL.Path)
		requests.Add(1)

		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.NotEmpty(t, req.Messages)

		// The scorer prompt embeds the input JSON array after the marker.
		const marker = "Input memories (JSON array):\n"
		content := req.Messages[0].Content
		pos := strings.Index(content, marker)
		require.NotEqual(t, -1, pos)
		inputJSON := content[pos+len(marker):]
		var texts []string
		require.NoError(t, json.Unmarshal([]byte(inputJSON), &texts))

		scores := make([]float64, 0, len(texts))
		for _, text := range texts {
			scores = append(scores, float64(len(text))/10.0)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": mustJSON(map[string]any{"scores": scores}),
					},
				},
			},
		})
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-key", "openai/gpt-5-mini:nitro", 2*time.Second)
	require.NoError(t, err)
	scorer := NewScorer(client)

	inputs := []string{"a", "bb", "", "cccc", "ddddd"}
	scores, err := scorer.BatchScore(context.Background(), inputs)
	require.NoError(t, err)
	require.Equal(t, int32(2), requests.Load()) // one empty string is skipped
	require.Equal(t, []float64{0.1, 0.2, 0, 0.4, 0.5}, scores)
}

func TestBatchScoreParallelChunks(t *testing.T) {
	origChunk := openRouterMaxBatchScores
	origParallel := openRouterMaxParallelScorings
	openRouterMaxBatchScores = 1
	openRouterMaxParallelScorings = 4
	defer func() {
		openRouterMaxBatchScores = origChunk
		openRouterMaxParallelScorings = origParallel
	}()

	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := inFlight.Add(1)
		for {
			prev := maxInFlight.Load()
			if current <= prev || maxInFlight.CompareAndSwap(prev, current) {
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": `{"scores":[0.5]}`,
					},
				},
			},
		})
		inFlight.Add(-1)
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-key", "openai/gpt-5-mini:nitro", 2*time.Second)
	require.NoError(t, err)
	scorer := NewScorer(client)

	_, err = scorer.BatchScore(context.Background(), []string{"a", "b", "c", "d", "e", "f"})
	require.NoError(t, err)
	require.Greater(t, maxInFlight.Load(), int32(1))
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
