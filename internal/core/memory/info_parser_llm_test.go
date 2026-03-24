package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

// --- shared test helpers ---

// makeParserJSON builds the {"facts":[...]} JSON string used as the LLM payload.
func makeParserJSON(facts []map[string]any) string {
	b, _ := json.Marshal(map[string]any{"facts": facts})
	return string(b)
}

// makeOllamaBody wraps a parser-JSON string in the Ollama generate envelope:
// {"response": "<parser-json>"}.
func makeOllamaBody(parserJSON string) []byte {
	b, _ := json.Marshal(map[string]string{"response": parserJSON})
	return b
}

// newOllamaParserDirect constructs an ollamaInfoParser bypassing preflight,
// useful for unit tests that mock the HTTP server themselves.
func newOllamaParserDirect(baseURL, model string) *ollamaInfoParser {
	return &ollamaInfoParser{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{},
	}
}

// ===== ollamaInfoParser tests =====

func TestOllamaInfoParser_ParseEmptyContent(t *testing.T) {
	p := newOllamaParserDirect("http://unused", "m")
	facts, err := p.Parse(context.Background(), "   ", 5)
	require.NoError(t, err)
	require.Empty(t, facts)
}

func TestOllamaInfoParser_ParseZeroMaxFacts(t *testing.T) {
	p := newOllamaParserDirect("http://unused", "m")
	_, err := p.Parse(context.Background(), "Alice works at ACME Corp", 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max facts")
}

func TestOllamaInfoParser_ParseHappyPath(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{
			"content":  "Alice works at ACME Corp",
			"kind":     "observation",
			"tags":     []string{"work"},
			"entity":   "Alice",
			"relation": "worksAt",
			"value":    "ACME Corp",
		},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeOllamaBody(parserJSON))
	}))
	defer srv.Close()

	p := newOllamaParserDirect(srv.URL, "test-model")
	facts, err := p.Parse(context.Background(), "Alice works at ACME Corp", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "Alice works at ACME Corp", facts[0].Content)
	require.Equal(t, domain.MemoryKindObservation, facts[0].Kind)
	require.Equal(t, "Alice", facts[0].Entity)
	require.Equal(t, "worksAt", facts[0].Relation)
	require.Equal(t, "ACME Corp", facts[0].Value)
}

func TestOllamaInfoParser_ParseEventKind(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{
			"content":  "Alice got married to Bob last Saturday",
			"kind":     "event",
			"entity":   "Alice",
			"relation": "marriedTo",
			"value":    "Bob",
		},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeOllamaBody(parserJSON))
	}))
	defer srv.Close()

	p := newOllamaParserDirect(srv.URL, "test-model")
	facts, err := p.Parse(context.Background(), "Alice got married to Bob last Saturday", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, domain.MemoryKindEvent, facts[0].Kind)
	require.Contains(t, facts[0].Tags, "event")
}

func TestOllamaInfoParser_ParseLimitsToMaxFacts(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "Alice enjoys hiking in the mountains every weekend", "kind": "observation"},
		{"content": "Bob drives a red Toyota pickup truck to work", "kind": "observation"},
		{"content": "Charlie studied economics at MIT university in Boston", "kind": "observation"},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeOllamaBody(parserJSON))
	}))
	defer srv.Close()

	p := newOllamaParserDirect(srv.URL, "test-model")
	facts, err := p.Parse(context.Background(), "some content text here", 2)
	require.NoError(t, err)
	require.Len(t, facts, 2)
}

func TestOllamaInfoParser_ParseDeduplicatesFacts(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "Alice enjoys hiking in the mountains on weekends", "kind": "observation"},
		{"content": "Alice enjoys hiking in the mountains on weekends", "kind": "observation"},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeOllamaBody(parserJSON))
	}))
	defer srv.Close()

	p := newOllamaParserDirect(srv.URL, "test-model")
	facts, err := p.Parse(context.Background(), "some content text here", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
}

func TestOllamaInfoParser_ParseFiltersLowSignalFacts(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "ok", "kind": "observation"},
		{"content": "Alice graduated from Stanford University in 2018", "kind": "observation"},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeOllamaBody(parserJSON))
	}))
	defer srv.Close()

	p := newOllamaParserDirect(srv.URL, "test-model")
	facts, err := p.Parse(context.Background(), "some content", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "Alice graduated from Stanford University in 2018", facts[0].Content)
}

func TestOllamaInfoParser_ParseHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer srv.Close()

	p := newOllamaParserDirect(srv.URL, "test-model")
	_, err := p.Parse(context.Background(), "Alice works at ACME Corp", 5)
	require.Error(t, err)
}

func TestOllamaInfoParser_ParseInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeOllamaBody("not json at all"))
	}))
	defer srv.Close()

	p := newOllamaParserDirect(srv.URL, "test-model")
	_, err := p.Parse(context.Background(), "Alice works at ACME Corp", 5)
	require.Error(t, err)
}

func TestOllamaInfoParser_ParseInfersEntityWhenMissing(t *testing.T) {
	// Facts without entity/relation/value should still be returned (inference attempted).
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "Alice lives in Seattle and works remotely", "kind": "observation"},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeOllamaBody(parserJSON))
	}))
	defer srv.Close()

	p := newOllamaParserDirect(srv.URL, "test-model")
	facts, err := p.Parse(context.Background(), "Alice lives in Seattle and works remotely", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "Alice lives in Seattle and works remotely", facts[0].Content)
}

func TestOllamaInfoParser_ParsePreservesProvidedRelationValueWhenEntityMissing(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "I use TypeScript", "kind": "observation", "relation": "tool", "value": "TypeScript"},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(makeOllamaBody(parserJSON))
	}))
	defer srv.Close()

	p := newOllamaParserDirect(srv.URL, "test-model")
	facts, err := p.Parse(context.Background(), "I use TypeScript", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "user", strings.ToLower(facts[0].Entity))
	require.Equal(t, "tool", facts[0].Relation)
	require.Equal(t, "TypeScript", facts[0].Value)
}

// NewOllamaInfoParser constructor (with real preflight via httptest).

func TestNewOllamaInfoParser_PrefightSucceeds(t *testing.T) {
	const model = "qwen2.5:7b"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"0.5.0"}`))
		case "/api/tags":
			b, _ := json.Marshal(map[string]any{
				"models": []map[string]any{{"name": model}},
			})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(b)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	parser, err := NewOllamaInfoParser(srv.URL, model, 0, nil, false)
	require.NoError(t, err)
	require.NotNil(t, parser)
}

func TestNewOllamaInfoParser_PrefightFailsModelMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"version":"0.5.0"}`))
		case "/api/tags":
			b, _ := json.Marshal(map[string]any{
				"models": []map[string]any{{"name": "some-other-model"}},
			})
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(b)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	_, err := NewOllamaInfoParser(srv.URL, "missing-model", 0, nil, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not available")
}

func TestNewOllamaInfoParser_PrefightFailsVersionEndpointDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := NewOllamaInfoParser(srv.URL, "some-model", 0, nil, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "connect to parser ollama")
}

// ===== openRouterInfoParser tests =====

// mockOpenRouterClient is a test double for the openRouterGenerator interface.
type mockOpenRouterClient struct {
	generateFn func(ctx context.Context, prompt string) (string, error)
	model      string
}

func (m *mockOpenRouterClient) Generate(ctx context.Context, prompt string) (string, error) {
	return m.generateFn(ctx, prompt)
}

func (m *mockOpenRouterClient) Model() string { return m.model }

// newOpenRouterParserWithMock builds an openRouterInfoParser backed by the given mock.
func newOpenRouterParserWithMock(generateFn func(context.Context, string) (string, error), model string) InfoParser {
	return &openRouterInfoParser{
		client: &mockOpenRouterClient{generateFn: generateFn, model: model},
	}
}

func TestOpenRouterInfoParser_ParseEmptyContent(t *testing.T) {
	called := false
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		called = true
		return "", nil
	}, "gpt-oss-20b:nitro")
	facts, err := p.Parse(context.Background(), "   ", 5)
	require.NoError(t, err)
	require.Empty(t, facts)
	require.False(t, called, "Generate must not be called for empty content")
}

func TestOpenRouterInfoParser_ParseZeroMaxFacts(t *testing.T) {
	p := newOpenRouterParserWithMock(nil, "gpt-oss-20b:nitro")
	_, err := p.Parse(context.Background(), "Alice works at ACME Corp", 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max facts")
}

func TestOpenRouterInfoParser_ParseNilClient(t *testing.T) {
	p := &openRouterInfoParser{client: nil}
	_, err := p.Parse(context.Background(), "Alice works at ACME Corp", 5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil")
}

func TestOpenRouterInfoParser_ParseHappyPath(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{
			"content":  "Alice works at ACME Corp",
			"kind":     "observation",
			"tags":     []string{"work"},
			"entity":   "Alice",
			"relation": "worksAt",
			"value":    "ACME Corp",
		},
	})
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return parserJSON, nil
	}, "openai/gpt-oss-20b:nitro")

	facts, err := p.Parse(context.Background(), "Alice works at ACME Corp", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "Alice works at ACME Corp", facts[0].Content)
	require.Equal(t, domain.MemoryKindObservation, facts[0].Kind)
	require.Equal(t, "Alice", facts[0].Entity)
	require.Equal(t, "worksAt", facts[0].Relation)
	require.Equal(t, "ACME Corp", facts[0].Value)
}

func TestOpenRouterInfoParser_ParseEventKind(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{
			"content":  "Alice got married to Bob last Saturday",
			"kind":     "event",
			"entity":   "Alice",
			"relation": "marriedTo",
			"value":    "Bob",
		},
	})
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return parserJSON, nil
	}, "openai/gpt-oss-20b:nitro")

	facts, err := p.Parse(context.Background(), "Alice got married to Bob last Saturday", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, domain.MemoryKindEvent, facts[0].Kind)
	require.Contains(t, facts[0].Tags, "event")
}

func TestOpenRouterInfoParser_ParseLimitsToMaxFacts(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "Alice enjoys hiking in the mountains every weekend", "kind": "observation"},
		{"content": "Bob drives a red Toyota pickup truck to work", "kind": "observation"},
		{"content": "Charlie studied economics at MIT university in Boston", "kind": "observation"},
	})
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return parserJSON, nil
	}, "openai/gpt-oss-20b:nitro")

	facts, err := p.Parse(context.Background(), "some content here", 2)
	require.NoError(t, err)
	require.Len(t, facts, 2)
}

func TestOpenRouterInfoParser_ParseDeduplicatesFacts(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "Alice enjoys hiking in the mountains on weekends", "kind": "observation"},
		{"content": "Alice enjoys hiking in the mountains on weekends", "kind": "observation"},
	})
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return parserJSON, nil
	}, "openai/gpt-oss-20b:nitro")

	facts, err := p.Parse(context.Background(), "some content here", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
}

func TestOpenRouterInfoParser_ParseFiltersLowSignalFacts(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "ok", "kind": "observation"},
		{"content": "Alice graduated from Stanford University in 2018", "kind": "observation"},
	})
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return parserJSON, nil
	}, "openai/gpt-oss-20b:nitro")

	facts, err := p.Parse(context.Background(), "some content here", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "Alice graduated from Stanford University in 2018", facts[0].Content)
}

func TestOpenRouterInfoParser_ParseGenerateError(t *testing.T) {
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return "", fmt.Errorf("upstream provider error")
	}, "openai/gpt-oss-20b:nitro")

	_, err := p.Parse(context.Background(), "Alice works at ACME Corp", 5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream provider error")
}

func TestOpenRouterInfoParser_ParseInvalidJSON(t *testing.T) {
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return "not json at all", nil
	}, "openai/gpt-oss-20b:nitro")

	_, err := p.Parse(context.Background(), "Alice works at ACME Corp", 5)
	require.Error(t, err)
}

func TestOpenRouterInfoParser_ParseJSONEmbeddedInText(t *testing.T) {
	// decodeParserJSON can extract JSON embedded in surrounding text (e.g. model preamble).
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "Alice works at ACME Corp", "kind": "observation", "entity": "Alice", "relation": "worksAt", "value": "ACME Corp"},
	})
	wrapped := "Sure, here is the result:\n" + parserJSON + "\nHope that helps!"

	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return wrapped, nil
	}, "openai/gpt-oss-20b:nitro")

	facts, err := p.Parse(context.Background(), "Alice works at ACME Corp", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
}

func TestOpenRouterInfoParser_ParseInfersEntityWhenMissing(t *testing.T) {
	// Facts without entity/relation/value should still be returned.
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "Alice lives in Seattle and works remotely", "kind": "observation"},
	})
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return parserJSON, nil
	}, "openai/gpt-oss-20b:nitro")

	facts, err := p.Parse(context.Background(), "Alice lives in Seattle and works remotely", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "Alice lives in Seattle and works remotely", facts[0].Content)
}

func TestOpenRouterInfoParser_ParsePreservesProvidedRelationValueWhenEntityMissing(t *testing.T) {
	parserJSON := makeParserJSON([]map[string]any{
		{"content": "I use TypeScript", "kind": "observation", "relation": "tool", "value": "TypeScript"},
	})
	p := newOpenRouterParserWithMock(func(_ context.Context, _ string) (string, error) {
		return parserJSON, nil
	}, "openai/gpt-oss-20b:nitro")

	facts, err := p.Parse(context.Background(), "I use TypeScript", 5)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "user", strings.ToLower(facts[0].Entity))
	require.Equal(t, "tool", facts[0].Relation)
	require.Equal(t, "TypeScript", facts[0].Value)
}
