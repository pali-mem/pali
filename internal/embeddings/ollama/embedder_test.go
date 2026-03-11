package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewEmbedderAndEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			require.Equal(t, http.MethodGet, r.Method)
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "0.6.0"})
		case "/api/tags":
			require.Equal(t, http.MethodGet, r.Method)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "all-minilm:latest"},
				},
			})
		case "/api/embed":
			require.Equal(t, http.MethodPost, r.Method)
			var in map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
			require.Equal(t, "all-minilm", in["model"])
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float32{{0.1, 0.2, 0.3}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e, err := NewEmbedder(srv.URL, "all-minilm", 2*time.Second)
	require.NoError(t, err)

	vec, err := e.Embed(context.Background(), "hello world")
	require.NoError(t, err)
	require.Equal(t, []float32{0.1, 0.2, 0.3}, vec)
}

func TestBatchEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			require.Equal(t, http.MethodGet, r.Method)
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "0.6.0"})
		case "/api/tags":
			require.Equal(t, http.MethodGet, r.Method)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "all-minilm:latest"},
				},
			})
		case "/api/embed":
			require.Equal(t, http.MethodPost, r.Method)
			var in map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
			require.Equal(t, "all-minilm", in["model"])
			inputs, ok := in["input"].([]any)
			require.True(t, ok)
			require.Len(t, inputs, 2)
			require.Equal(t, "first", inputs[0])
			require.Equal(t, "second", inputs[1])
			_ = json.NewEncoder(w).Encode(map[string]any{
				"embeddings": [][]float32{
					{0.1, 0.2, 0.3},
					{0.4, 0.5, 0.6},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e, err := NewEmbedder(srv.URL, "all-minilm", 2*time.Second)
	require.NoError(t, err)

	vectors, err := e.BatchEmbed(context.Background(), []string{"first", "second"})
	require.NoError(t, err)
	require.Len(t, vectors, 2)
	require.Equal(t, []float32{0.1, 0.2, 0.3}, vectors[0])
	require.Equal(t, []float32{0.4, 0.5, 0.6}, vectors[1])
}

func TestNewEmbedderMissingModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_ = json.NewEncoder(w).Encode(map[string]any{"version": "0.6.0"})
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "nomic-embed-text:latest"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	_, err := NewEmbedder(srv.URL, "all-minilm", 2*time.Second)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ollama pull all-minilm")
}

func TestNewEmbedderUnavailableServer(t *testing.T) {
	_, err := NewEmbedder("http://127.0.0.1:65533", "all-minilm", 100*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Install Ollama")
	require.Contains(t, err.Error(), "ollama serve")
}
