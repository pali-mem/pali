package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewEmbedderAndBatchEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/embeddings", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var in map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.Equal(t, "openai/text-embedding-3-small:nitro", in["model"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 1, "embedding": []float32{0.4, 0.5}},
				{"index": 0, "embedding": []float32{0.1, 0.2}},
			},
		})
	}))
	defer srv.Close()

	e, err := NewEmbedder(srv.URL, "test-key", "openai/text-embedding-3-small:nitro", 2*time.Second)
	require.NoError(t, err)

	vectors, err := e.BatchEmbed(context.Background(), []string{"first", "second"})
	require.NoError(t, err)
	require.Len(t, vectors, 2)
	require.Equal(t, []float32{0.1, 0.2}, vectors[0])
	require.Equal(t, []float32{0.4, 0.5}, vectors[1])
}

func TestNewEmbedderRequiresAPIKey(t *testing.T) {
	_, err := NewEmbedder("https://openrouter.ai/api/v1", "", "openai/text-embedding-3-small:nitro", time.Second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "api key")
}

func TestBatchEmbedBadCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"index": 0, "embedding": []float32{0.1, 0.2}},
			},
		})
	}))
	defer srv.Close()

	e, err := NewEmbedder(srv.URL, "test-key", "openai/text-embedding-3-small:nitro", 2*time.Second)
	require.NoError(t, err)

	_, err = e.BatchEmbed(context.Background(), []string{"first", "second"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "count mismatch")
}

func TestBatchEmbedChunkingPreservesOrder(t *testing.T) {
	origBatchSize := openRouterMaxBatchInputs
	origParallel := openRouterMaxParallelBatches
	openRouterMaxBatchInputs = 2
	openRouterMaxParallelBatches = 2
	defer func() {
		openRouterMaxBatchInputs = origBatchSize
		openRouterMaxParallelBatches = origParallel
	}()

	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Input []string `json:"input"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		requests.Add(1)
		data := make([]map[string]any, 0, len(in.Input))
		for i := range in.Input {
			data = append(data, map[string]any{
				"index":     i,
				"embedding": []float32{float32(len(in.Input[i]))},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	e, err := NewEmbedder(srv.URL, "test-key", "openai/text-embedding-3-small:nitro", 2*time.Second)
	require.NoError(t, err)

	inputs := []string{"a", "bb", "ccc", "dddd", "eeeee"}
	out, err := e.BatchEmbed(context.Background(), inputs)
	require.NoError(t, err)
	require.Equal(t, int32(3), requests.Load())
	require.Len(t, out, len(inputs))
	require.Equal(t, []float32{1}, out[0])
	require.Equal(t, []float32{2}, out[1])
	require.Equal(t, []float32{3}, out[2])
	require.Equal(t, []float32{4}, out[3])
	require.Equal(t, []float32{5}, out[4])
}

func TestBatchEmbedParallelChunks(t *testing.T) {
	origBatchSize := openRouterMaxBatchInputs
	origParallel := openRouterMaxParallelBatches
	openRouterMaxBatchInputs = 1
	openRouterMaxParallelBatches = 4
	defer func() {
		openRouterMaxBatchInputs = origBatchSize
		openRouterMaxParallelBatches = origParallel
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
			"data": []map[string]any{
				{"index": 0, "embedding": []float32{1}},
			},
		})
		inFlight.Add(-1)
	}))
	defer srv.Close()

	e, err := NewEmbedder(srv.URL, "test-key", "openai/text-embedding-3-small:nitro", 2*time.Second)
	require.NoError(t, err)

	_, err = e.BatchEmbed(context.Background(), []string{"a", "b", "c", "d", "e", "f"})
	require.NoError(t, err)
	require.Greater(t, maxInFlight.Load(), int32(1))
}
