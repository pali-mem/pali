package openrouter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": "0.75",
					},
				},
			},
		})
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-key", "openai/gpt-5-mini:nitro", 2*time.Second)
	require.NoError(t, err)

	out, err := client.Generate(context.Background(), "score this")
	require.NoError(t, err)
	require.Equal(t, "0.75", out)
}

func TestGenerateWithMultipartContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"content": []map[string]any{
							{"type": "text", "text": "0."},
							{"type": "text", "text": "42"},
						},
					},
				},
			},
		})
	}))
	defer srv.Close()

	client, err := NewClient(srv.URL, "test-key", "openai/gpt-5-mini:nitro", 2*time.Second)
	require.NoError(t, err)

	out, err := client.Generate(context.Background(), "score this")
	require.NoError(t, err)
	require.Equal(t, "0.42", out)
}
