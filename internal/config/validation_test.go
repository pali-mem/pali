package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate_OllamaModelRequired(t *testing.T) {
	cfg := Defaults()
	cfg.Embedding.Provider = "ollama"
	cfg.Embedding.OllamaModel = ""
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "embedding.ollama_model")
}

func TestValidate_ONNXPathsRequired(t *testing.T) {
	cfg := Defaults()
	cfg.Embedding.Provider = "onnx"
	cfg.Embedding.ModelPath = ""
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "embedding.model_path")
}

func TestValidate_FallbackProviderSupported(t *testing.T) {
	cfg := Defaults()
	cfg.Embedding.FallbackProvider = "lexical"
	require.NoError(t, Validate(cfg))

	cfg.Embedding.FallbackProvider = "unknown"
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "embedding.fallback_provider")
}

func TestValidate_ImportanceScorerSupported(t *testing.T) {
	cfg := Defaults()
	cfg.ImportanceScorer = "heuristic"
	require.NoError(t, Validate(cfg))

	cfg.ImportanceScorer = "ollama"
	cfg.Ollama.Model = "llama3.1:8b"
	require.NoError(t, Validate(cfg))

	cfg.ImportanceScorer = "unknown"
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "importance_scorer")
}

func TestValidate_OllamaScorerModelRequired(t *testing.T) {
	cfg := Defaults()
	cfg.ImportanceScorer = "ollama"
	cfg.Ollama.Model = ""

	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ollama.model")
}

func TestValidate_QdrantBackendRequiresValidConfig(t *testing.T) {
	cfg := Defaults()
	cfg.VectorBackend = "qdrant"
	require.NoError(t, Validate(cfg))

	cfg.Qdrant.BaseURL = "://bad"
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "qdrant.base_url")

	cfg = Defaults()
	cfg.VectorBackend = "qdrant"
	cfg.Qdrant.Collection = ""
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "qdrant.collection")
}

func TestValidate_StructuredMemoryOptions(t *testing.T) {
	cfg := Defaults()
	cfg.StructuredMemory.MaxObservations = -1
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "structured_memory.max_observations")

	cfg = Defaults()
	cfg.StructuredMemory.DualWriteObservations = true
	cfg.StructuredMemory.MaxObservations = 0
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dual_write_observations")

	cfg = Defaults()
	cfg.StructuredMemory.DualWriteEvents = true
	cfg.StructuredMemory.MaxObservations = 0
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dual_write_events")
}

func TestValidate_RetrievalScoringAlgorithm(t *testing.T) {
	cfg := Defaults()
	cfg.Retrieval.Scoring.Algorithm = "wal"
	require.NoError(t, Validate(cfg))

	cfg.Retrieval.Scoring.Algorithm = "match"
	require.NoError(t, Validate(cfg))

	cfg.Retrieval.Scoring.Algorithm = "unknown"
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.scoring.algorithm")
}

func TestValidate_ParserOptions(t *testing.T) {
	cfg := Defaults()
	cfg.Parser.MaxFacts = 0
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parser.max_facts")

	cfg = Defaults()
	cfg.Parser.Enabled = true
	cfg.Parser.Provider = "ollama"
	cfg.Parser.OllamaModel = ""
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parser.ollama_model")

	cfg = Defaults()
	cfg.Parser.DedupeThreshold = 0.95
	cfg.Parser.UpdateThreshold = 0.90
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parser.dedupe_threshold")
}
