package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	require.Equal(t, 8080, cfg.Server.Port)
	require.Equal(t, "", cfg.DefaultTenantID)
	require.Equal(t, "heuristic", cfg.ImportanceScorer)
	require.True(t, cfg.Postprocess.Enabled)
	require.Equal(t, 250, cfg.Postprocess.PollIntervalMS)
	require.Equal(t, 32, cfg.Postprocess.BatchSize)
	require.Equal(t, 2, cfg.Postprocess.WorkerCount)
	require.Equal(t, 30000, cfg.Postprocess.LeaseMS)
	require.Equal(t, 5, cfg.Postprocess.MaxAttempts)
	require.Equal(t, 500, cfg.Postprocess.RetryBaseMS)
	require.Equal(t, 60000, cfg.Postprocess.RetryMaxMS)
	require.False(t, cfg.StructuredMemory.Enabled)
	require.False(t, cfg.StructuredMemory.DualWriteObservations)
	require.False(t, cfg.StructuredMemory.DualWriteEvents)
	require.Equal(t, 3, cfg.StructuredMemory.MaxObservations)
	require.Equal(t, "wal", cfg.Retrieval.Scoring.Algorithm)
	require.Equal(t, 0.1, cfg.Retrieval.Scoring.WAL.Recency)
	require.Equal(t, 0.8, cfg.Retrieval.Scoring.WAL.Relevance)
	require.Equal(t, 0.1, cfg.Retrieval.Scoring.WAL.Importance)
	require.False(t, cfg.Parser.Enabled)
	require.Equal(t, "heuristic", cfg.Parser.Provider)
	require.Equal(t, 4, cfg.Parser.MaxFacts)
	require.Equal(t, "ollama", cfg.Embedding.Provider)
	require.Equal(t, "", cfg.Embedding.FallbackProvider)
	require.Equal(t, "mxbai-embed-large", cfg.Embedding.OllamaModel)
	require.Equal(t, "http://127.0.0.1:6333", cfg.Qdrant.BaseURL)
	require.Equal(t, "pali_memories", cfg.Qdrant.Collection)
	require.Equal(t, 2000, cfg.Qdrant.TimeoutMS)
	require.Equal(t, "http://127.0.0.1:11434", cfg.Ollama.BaseURL)
	require.Equal(t, "deepseek-r1:7b", cfg.Ollama.Model)
	require.Equal(t, 2000, cfg.Ollama.TimeoutMS)
}
