package config

import (
	"fmt"
	"net/url"
	"strings"
)

func Validate(cfg Config) error {
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("invalid server.port: %d", cfg.Server.Port)
	}
	switch cfg.VectorBackend {
	case "sqlite", "qdrant", "pgvector":
	default:
		return fmt.Errorf("invalid vector_backend: %q", cfg.VectorBackend)
	}
	provider := normalizeProvider(cfg.Embedding.Provider)
	if !isSupportedEmbeddingProvider(provider) {
		return fmt.Errorf("invalid embedding.provider: %q", cfg.Embedding.Provider)
	}

	fallbackProvider := normalizeProvider(cfg.Embedding.FallbackProvider)
	if fallbackProvider != "" && !isSupportedEmbeddingProvider(fallbackProvider) {
		return fmt.Errorf("invalid embedding.fallback_provider: %q", cfg.Embedding.FallbackProvider)
	}

	if provider == "onnx" {
		if strings.TrimSpace(cfg.Embedding.ModelPath) == "" {
			return fmt.Errorf("embedding.model_path is required when embedding.provider=onnx")
		}
		if strings.TrimSpace(cfg.Embedding.TokenizerPath) == "" {
			return fmt.Errorf("embedding.tokenizer_path is required when embedding.provider=onnx")
		}
	}
	if provider == "ollama" {
		if strings.TrimSpace(cfg.Embedding.OllamaModel) == "" {
			return fmt.Errorf("embedding.ollama_model is required when embedding.provider=ollama")
		}
		if cfg.Embedding.OllamaTimeoutSeconds < 0 {
			return fmt.Errorf("embedding.ollama_timeout_seconds must be >= 0")
		}
	}

	importanceScorer := normalizeImportanceScorer(cfg.ImportanceScorer)
	if !isSupportedImportanceScorer(importanceScorer) {
		return fmt.Errorf("invalid importance_scorer: %q", cfg.ImportanceScorer)
	}
	if importanceScorer == "ollama" {
		if strings.TrimSpace(cfg.Ollama.Model) == "" {
			return fmt.Errorf("ollama.model is required when importance_scorer=ollama")
		}
		if cfg.Ollama.TimeoutMS < 0 {
			return fmt.Errorf("ollama.timeout_ms must be >= 0")
		}
	}

	rankingAlgorithm := normalizeRankingAlgorithm(cfg.Retrieval.Scoring.Algorithm)
	if !isSupportedRankingAlgorithm(rankingAlgorithm) {
		return fmt.Errorf("invalid retrieval.scoring.algorithm: %q", cfg.Retrieval.Scoring.Algorithm)
	}
	if err := validateScoringWeights(cfg.Retrieval.Scoring.WAL, "retrieval.scoring.wal"); err != nil {
		return err
	}
	if err := validateMatchScoringWeights(cfg.Retrieval.Scoring.Match, "retrieval.scoring.match"); err != nil {
		return err
	}

	if cfg.VectorBackend == "sqlite" && strings.TrimSpace(cfg.Database.SQLiteDSN) == "" {
		return fmt.Errorf("database.sqlite_dsn is required when vector_backend=sqlite")
	}
	if cfg.VectorBackend == "qdrant" {
		if strings.TrimSpace(cfg.Qdrant.BaseURL) == "" {
			return fmt.Errorf("qdrant.base_url is required when vector_backend=qdrant")
		}
		if _, err := url.ParseRequestURI(cfg.Qdrant.BaseURL); err != nil {
			return fmt.Errorf("invalid qdrant.base_url: %w", err)
		}
		if strings.TrimSpace(cfg.Qdrant.Collection) == "" {
			return fmt.Errorf("qdrant.collection is required when vector_backend=qdrant")
		}
		if cfg.Qdrant.TimeoutMS < 0 {
			return fmt.Errorf("qdrant.timeout_ms must be >= 0")
		}
	}
	if cfg.Auth.Enabled && strings.TrimSpace(cfg.Auth.JWTSecret) == "" {
		return fmt.Errorf("auth.jwt_secret is required when auth.enabled=true")
	}
	if cfg.StructuredMemory.MaxObservations < 0 {
		return fmt.Errorf("structured_memory.max_observations must be >= 0")
	}
	if (cfg.StructuredMemory.DualWriteObservations || cfg.StructuredMemory.DualWriteEvents) && cfg.StructuredMemory.MaxObservations == 0 {
		return fmt.Errorf("structured_memory.max_observations must be > 0 when dual_write_observations=true or dual_write_events=true")
	}
	if cfg.Parser.MaxFacts <= 0 {
		return fmt.Errorf("parser.max_facts must be > 0")
	}
	if cfg.Parser.OllamaTimeoutMS < 0 {
		return fmt.Errorf("parser.ollama_timeout_ms must be >= 0")
	}
	if cfg.Parser.DedupeThreshold < 0 || cfg.Parser.DedupeThreshold > 1 {
		return fmt.Errorf("parser.dedupe_threshold must be in [0,1]")
	}
	if cfg.Parser.UpdateThreshold < 0 || cfg.Parser.UpdateThreshold > 1 {
		return fmt.Errorf("parser.update_threshold must be in [0,1]")
	}
	if cfg.Parser.DedupeThreshold > cfg.Parser.UpdateThreshold {
		return fmt.Errorf("parser.dedupe_threshold must be <= parser.update_threshold")
	}
	if cfg.Parser.Enabled {
		parserProvider := normalizeParserProvider(cfg.Parser.Provider)
		if !isSupportedParserProvider(parserProvider) {
			return fmt.Errorf("invalid parser.provider: %q", cfg.Parser.Provider)
		}
		if parserProvider == "ollama" {
			if strings.TrimSpace(cfg.Parser.OllamaBaseURL) == "" {
				return fmt.Errorf("parser.ollama_base_url is required when parser.provider=ollama")
			}
			if _, err := url.ParseRequestURI(cfg.Parser.OllamaBaseURL); err != nil {
				return fmt.Errorf("invalid parser.ollama_base_url: %w", err)
			}
			if strings.TrimSpace(cfg.Parser.OllamaModel) == "" {
				return fmt.Errorf("parser.ollama_model is required when parser.provider=ollama")
			}
		}
	}
	return nil
}

func normalizeProvider(in string) string {
	return strings.ToLower(strings.TrimSpace(in))
}

func normalizeImportanceScorer(in string) string {
	normalized := strings.ToLower(strings.TrimSpace(in))
	if normalized == "" {
		return "heuristic"
	}
	return normalized
}

func normalizeRankingAlgorithm(in string) string {
	normalized := strings.ToLower(strings.TrimSpace(in))
	if normalized == "" {
		return "wal"
	}
	return normalized
}

func normalizeParserProvider(in string) string {
	normalized := strings.ToLower(strings.TrimSpace(in))
	if normalized == "" {
		return "heuristic"
	}
	return normalized
}

func isSupportedEmbeddingProvider(provider string) bool {
	switch provider {
	case "lexical", "mock", "onnx", "ollama":
		return true
	default:
		return false
	}
}

func isSupportedImportanceScorer(provider string) bool {
	switch provider {
	case "heuristic", "ollama":
		return true
	default:
		return false
	}
}

func isSupportedRankingAlgorithm(algorithm string) bool {
	switch algorithm {
	case "wal", "match":
		return true
	default:
		return false
	}
}

func isSupportedParserProvider(provider string) bool {
	switch provider {
	case "heuristic", "ollama":
		return true
	default:
		return false
	}
}

func validateScoringWeights(weights ScoringWeightsConfig, path string) error {
	if weights.Recency < 0 || weights.Relevance < 0 || weights.Importance < 0 {
		return fmt.Errorf("%s weights must be >= 0", path)
	}
	if weights.Recency+weights.Relevance+weights.Importance == 0 {
		return fmt.Errorf("%s weights must not all be zero", path)
	}
	return nil
}

func validateMatchScoringWeights(weights MatchScoringWeightsConfig, path string) error {
	values := []float64{
		weights.Recency,
		weights.Relevance,
		weights.Importance,
		weights.QueryOverlap,
		weights.Routing,
	}
	total := 0.0
	for _, v := range values {
		if v < 0 {
			return fmt.Errorf("%s weights must be >= 0", path)
		}
		total += v
	}
	if total == 0 {
		return fmt.Errorf("%s weights must not all be zero", path)
	}
	return nil
}
