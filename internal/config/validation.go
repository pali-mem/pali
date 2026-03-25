package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Validate checks whether the configuration is internally consistent.
func Validate(cfg Config) error {
	if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
		return fmt.Errorf("invalid server.port: %d", cfg.Server.Port)
	}
	switch cfg.VectorBackend {
	case "sqlite", "qdrant", "pgvector":
	default:
		return fmt.Errorf("invalid vector_backend: %q", cfg.VectorBackend)
	}
	switch cfg.EntityFactBackend {
	case "sqlite", "neo4j":
	default:
		return fmt.Errorf("invalid entity_fact_backend: %q", cfg.EntityFactBackend)
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
	if provider == "openrouter" {
		if err := validateOpenRouterConfig(cfg, true, false); err != nil {
			return err
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
	if importanceScorer == "openrouter" {
		if err := validateOpenRouterConfig(cfg, false, true); err != nil {
			return err
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
	if cfg.Retrieval.Search.AdaptiveQueryMaxExtraQueries <= 0 {
		return fmt.Errorf("retrieval.search.adaptive_query_max_extra_queries must be > 0")
	}
	if cfg.Retrieval.Search.AdaptiveQueryWeakLexicalThreshold < 0 || cfg.Retrieval.Search.AdaptiveQueryWeakLexicalThreshold > 1 {
		return fmt.Errorf("retrieval.search.adaptive_query_weak_lexical_threshold must be in [0,1]")
	}
	if cfg.Retrieval.Search.AdaptiveQueryPlanConfidenceThreshold < 0 || cfg.Retrieval.Search.AdaptiveQueryPlanConfidenceThreshold > 1 {
		return fmt.Errorf("retrieval.search.adaptive_query_plan_confidence_threshold must be in [0,1]")
	}
	if cfg.Retrieval.Search.CandidateWindowMultiplier <= 0 {
		return fmt.Errorf("retrieval.search.candidate_window_multiplier must be > 0")
	}
	if cfg.Retrieval.Search.CandidateWindowMin <= 0 {
		return fmt.Errorf("retrieval.search.candidate_window_min must be > 0")
	}
	if cfg.Retrieval.Search.CandidateWindowMax <= 0 {
		return fmt.Errorf("retrieval.search.candidate_window_max must be > 0")
	}
	if cfg.Retrieval.Search.CandidateWindowMax < cfg.Retrieval.Search.CandidateWindowMin {
		return fmt.Errorf("retrieval.search.candidate_window_max must be >= retrieval.search.candidate_window_min")
	}
	if cfg.Retrieval.Search.CandidateWindowTemporalBoost < 0 {
		return fmt.Errorf("retrieval.search.candidate_window_temporal_boost must be >= 0")
	}
	if cfg.Retrieval.Search.CandidateWindowMultiHopBoost < 0 {
		return fmt.Errorf("retrieval.search.candidate_window_multi_hop_boost must be >= 0")
	}
	if cfg.Retrieval.Search.CandidateWindowFilterBoost < 0 {
		return fmt.Errorf("retrieval.search.candidate_window_filter_boost must be >= 0")
	}
	if cfg.Retrieval.Search.EarlyRerankBaseWindow <= 0 {
		return fmt.Errorf("retrieval.search.early_rerank_base_window must be > 0")
	}
	if cfg.Retrieval.Search.EarlyRerankMaxWindow <= 0 {
		return fmt.Errorf("retrieval.search.early_rerank_max_window must be > 0")
	}
	if cfg.Retrieval.Search.EarlyRerankMaxWindow < cfg.Retrieval.Search.EarlyRerankBaseWindow {
		return fmt.Errorf("retrieval.search.early_rerank_max_window must be >= retrieval.search.early_rerank_base_window")
	}
	if cfg.Retrieval.MultiHop.OllamaTimeoutMS < 0 {
		return fmt.Errorf("retrieval.multi_hop.ollama_timeout_ms must be >= 0")
	}
	if cfg.Retrieval.MultiHop.MaxDecompositionQueries <= 0 {
		return fmt.Errorf("retrieval.multi_hop.max_decomposition_queries must be > 0")
	}
	decompositionProvider := normalizeMultiHopDecompositionProvider(cfg.Retrieval.MultiHop.DecompositionProvider)
	if !isSupportedMultiHopDecompositionProvider(decompositionProvider) {
		return fmt.Errorf("invalid retrieval.multi_hop.decomposition_provider: %q", cfg.Retrieval.MultiHop.DecompositionProvider)
	}
	if cfg.Retrieval.MultiHop.LLMDecompositionEnabled {
		switch decompositionProvider {
		case "", "openrouter":
			if err := validateOpenRouterConfig(cfg, false, false); err != nil {
				return err
			}
			if strings.TrimSpace(cfg.Retrieval.MultiHop.OpenRouterModel) == "" &&
				strings.TrimSpace(cfg.OpenRouter.ScoringModel) == "" {
				return fmt.Errorf("retrieval.multi_hop.openrouter_model or openrouter.scoring_model is required when retrieval.multi_hop.llm_decomposition_enabled=true and decomposition_provider=openrouter")
			}
		case "ollama":
			if strings.TrimSpace(cfg.Retrieval.MultiHop.OllamaBaseURL) == "" {
				return fmt.Errorf("retrieval.multi_hop.ollama_base_url is required when retrieval.multi_hop.llm_decomposition_enabled=true and decomposition_provider=ollama")
			}
			if _, err := url.ParseRequestURI(cfg.Retrieval.MultiHop.OllamaBaseURL); err != nil {
				return fmt.Errorf("invalid retrieval.multi_hop.ollama_base_url: %w", err)
			}
			if strings.TrimSpace(cfg.Retrieval.MultiHop.OllamaModel) == "" {
				return fmt.Errorf("retrieval.multi_hop.ollama_model is required when retrieval.multi_hop.llm_decomposition_enabled=true and decomposition_provider=ollama")
			}
		case "none":
			return fmt.Errorf("retrieval.multi_hop.decomposition_provider=none cannot be used when retrieval.multi_hop.llm_decomposition_enabled=true")
		}
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
	if cfg.VectorBackend == "pgvector" {
		if strings.TrimSpace(cfg.PGVector.DSN) == "" {
			return fmt.Errorf("pgvector.dsn is required when vector_backend=pgvector")
		}
		if _, err := url.ParseRequestURI(cfg.PGVector.DSN); err != nil {
			return fmt.Errorf("invalid pgvector.dsn: %w", err)
		}
		if strings.TrimSpace(cfg.PGVector.Table) == "" {
			return fmt.Errorf("pgvector.table is required when vector_backend=pgvector")
		}
		if cfg.PGVector.MaxOpenConns < 0 {
			return fmt.Errorf("pgvector.max_open_conns must be >= 0")
		}
		if cfg.PGVector.MaxIdleConns < 0 {
			return fmt.Errorf("pgvector.max_idle_conns must be >= 0")
		}
	}
	if cfg.EntityFactBackend == "neo4j" {
		if strings.TrimSpace(cfg.Neo4j.URI) == "" {
			return fmt.Errorf("neo4j.uri is required when entity_fact_backend=neo4j")
		}
		if _, err := url.ParseRequestURI(cfg.Neo4j.URI); err != nil {
			return fmt.Errorf("invalid neo4j.uri: %w", err)
		}
		if strings.TrimSpace(cfg.Neo4j.Username) == "" {
			return fmt.Errorf("neo4j.username is required when entity_fact_backend=neo4j")
		}
		if strings.TrimSpace(cfg.Neo4j.Password) == "" {
			return fmt.Errorf("neo4j.password is required when entity_fact_backend=neo4j (or set NEO4J_PASSWORD)")
		}
		if cfg.Neo4j.TimeoutMS < 0 {
			return fmt.Errorf("neo4j.timeout_ms must be >= 0")
		}
		if cfg.Neo4j.BatchSize <= 0 {
			return fmt.Errorf("neo4j.batch_size must be > 0")
		}
	}
	if cfg.Retrieval.MultiHop.GraphMaxHops < 1 || cfg.Retrieval.MultiHop.GraphMaxHops > 4 {
		return fmt.Errorf("retrieval.multi_hop.graph_max_hops must be in [1,4]")
	}
	if cfg.Retrieval.MultiHop.GraphSeedLimit <= 0 {
		return fmt.Errorf("retrieval.multi_hop.graph_seed_limit must be > 0")
	}
	if cfg.Retrieval.MultiHop.GraphPathLimit <= 0 {
		return fmt.Errorf("retrieval.multi_hop.graph_path_limit must be > 0")
	}
	if cfg.Retrieval.MultiHop.GraphMinScore < 0 || cfg.Retrieval.MultiHop.GraphMinScore > 1 {
		return fmt.Errorf("retrieval.multi_hop.graph_min_score must be in [0,1]")
	}
	if cfg.Retrieval.MultiHop.GraphWeight < 0 || cfg.Retrieval.MultiHop.GraphWeight > 1 {
		return fmt.Errorf("retrieval.multi_hop.graph_weight must be in [0,1]")
	}
	if cfg.Auth.Enabled && strings.TrimSpace(cfg.Auth.JWTSecret) == "" {
		return fmt.Errorf("auth.jwt_secret is required when auth.enabled=true")
	}
	if cfg.Postprocess.PollIntervalMS <= 0 {
		return fmt.Errorf("postprocess.poll_interval_ms must be > 0")
	}
	if cfg.Postprocess.BatchSize <= 0 {
		return fmt.Errorf("postprocess.batch_size must be > 0")
	}
	if cfg.Postprocess.WorkerCount <= 0 {
		return fmt.Errorf("postprocess.worker_count must be > 0")
	}
	if cfg.Postprocess.LeaseMS <= 0 {
		return fmt.Errorf("postprocess.lease_ms must be > 0")
	}
	if cfg.Postprocess.MaxAttempts <= 0 {
		return fmt.Errorf("postprocess.max_attempts must be > 0")
	}
	if cfg.Postprocess.RetryBaseMS <= 0 {
		return fmt.Errorf("postprocess.retry_base_ms must be > 0")
	}
	if cfg.Postprocess.RetryMaxMS <= 0 {
		return fmt.Errorf("postprocess.retry_max_ms must be > 0")
	}
	if cfg.Postprocess.RetryBaseMS > cfg.Postprocess.RetryMaxMS {
		return fmt.Errorf("postprocess.retry_base_ms must be <= postprocess.retry_max_ms")
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
		if parserProvider == "openrouter" {
			if err := validateOpenRouterConfig(cfg, false, false); err != nil {
				return err
			}
			if strings.TrimSpace(cfg.Parser.OpenRouterModel) == "" && strings.TrimSpace(cfg.OpenRouter.ScoringModel) == "" {
				return fmt.Errorf("parser.openrouter_model or openrouter.scoring_model is required when parser.provider=openrouter")
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

func normalizeMultiHopDecompositionProvider(in string) string {
	normalized := strings.ToLower(strings.TrimSpace(in))
	if normalized == "disabled" {
		return "none"
	}
	return normalized
}

func isSupportedEmbeddingProvider(provider string) bool {
	switch provider {
	case "lexical", "mock", "onnx", "ollama", "openrouter":
		return true
	default:
		return false
	}
}

func isSupportedImportanceScorer(provider string) bool {
	switch provider {
	case "heuristic", "ollama", "openrouter":
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
	case "heuristic", "ollama", "openrouter":
		return true
	default:
		return false
	}
}

func isSupportedMultiHopDecompositionProvider(provider string) bool {
	switch provider {
	case "", "openrouter", "ollama", "none":
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

func validateOpenRouterConfig(cfg Config, requireEmbeddingModel, requireScoringModel bool) error {
	if strings.TrimSpace(cfg.OpenRouter.APIKey) == "" {
		return fmt.Errorf("openrouter.api_key is required when using openrouter providers (or set OPENROUTER_API_KEY)")
	}
	if strings.TrimSpace(cfg.OpenRouter.BaseURL) == "" {
		return fmt.Errorf("openrouter.base_url is required when using openrouter providers")
	}
	if _, err := url.ParseRequestURI(cfg.OpenRouter.BaseURL); err != nil {
		return fmt.Errorf("invalid openrouter.base_url: %w", err)
	}
	if cfg.OpenRouter.TimeoutMS < 0 {
		return fmt.Errorf("openrouter.timeout_ms must be >= 0")
	}
	if requireEmbeddingModel && strings.TrimSpace(cfg.OpenRouter.EmbeddingModel) == "" {
		return fmt.Errorf("openrouter.embedding_model is required when embedding.provider=openrouter")
	}
	if requireScoringModel && strings.TrimSpace(cfg.OpenRouter.ScoringModel) == "" {
		return fmt.Errorf("openrouter.scoring_model is required when importance_scorer=openrouter")
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
