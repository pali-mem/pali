package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func applyEnvironment(cfg *Config) error {
	if cfg == nil {
		return nil
	}

	applyLegacyEnvironmentFallbacks(cfg)

	overrides := envOverrides{}

	overrides.string("PALI_SERVER_HOST", &cfg.Server.Host)
	overrides.int("PALI_SERVER_PORT", &cfg.Server.Port)

	overrides.string("PALI_VECTOR_BACKEND", &cfg.VectorBackend)
	overrides.string("PALI_ENTITY_FACT_BACKEND", &cfg.EntityFactBackend)
	overrides.string("PALI_DEFAULT_TENANT_ID", &cfg.DefaultTenantID)
	overrides.string("PALI_IMPORTANCE_SCORER", &cfg.ImportanceScorer)

	overrides.bool("PALI_POSTPROCESS_ENABLED", &cfg.Postprocess.Enabled)
	overrides.int("PALI_POSTPROCESS_POLL_INTERVAL_MS", &cfg.Postprocess.PollIntervalMS)
	overrides.int("PALI_POSTPROCESS_BATCH_SIZE", &cfg.Postprocess.BatchSize)
	overrides.int("PALI_POSTPROCESS_WORKER_COUNT", &cfg.Postprocess.WorkerCount)
	overrides.int("PALI_POSTPROCESS_LEASE_MS", &cfg.Postprocess.LeaseMS)
	overrides.int("PALI_POSTPROCESS_MAX_ATTEMPTS", &cfg.Postprocess.MaxAttempts)
	overrides.int("PALI_POSTPROCESS_RETRY_BASE_MS", &cfg.Postprocess.RetryBaseMS)
	overrides.int("PALI_POSTPROCESS_RETRY_MAX_MS", &cfg.Postprocess.RetryMaxMS)

	overrides.bool("PALI_STRUCTURED_MEMORY_ENABLED", &cfg.StructuredMemory.Enabled)
	overrides.bool("PALI_STRUCTURED_MEMORY_DUAL_WRITE_OBSERVATIONS", &cfg.StructuredMemory.DualWriteObservations)
	overrides.bool("PALI_STRUCTURED_MEMORY_DUAL_WRITE_EVENTS", &cfg.StructuredMemory.DualWriteEvents)
	overrides.int("PALI_STRUCTURED_MEMORY_MAX_OBSERVATIONS", &cfg.StructuredMemory.MaxObservations)

	overrides.bool("PALI_RETRIEVAL_ANSWER_TYPE_ROUTING_ENABLED", &cfg.Retrieval.AnswerTypeRoutingEnabled)
	overrides.bool("PALI_RETRIEVAL_EARLY_RANK_RERANK_ENABLED", &cfg.Retrieval.EarlyRankRerankEnabled)
	overrides.bool("PALI_RETRIEVAL_TEMPORAL_RESOLVER_ENABLED", &cfg.Retrieval.TemporalResolverEnabled)
	overrides.bool("PALI_RETRIEVAL_OPEN_DOMAIN_ALTERNATIVE_RESOLVER_ENABLED", &cfg.Retrieval.OpenDomainAlternativeResolverEnabled)
	overrides.string("PALI_RETRIEVAL_SCORING_ALGORITHM", &cfg.Retrieval.Scoring.Algorithm)
	overrides.float64("PALI_RETRIEVAL_SCORING_WAL_RECENCY", &cfg.Retrieval.Scoring.WAL.Recency)
	overrides.float64("PALI_RETRIEVAL_SCORING_WAL_RELEVANCE", &cfg.Retrieval.Scoring.WAL.Relevance)
	overrides.float64("PALI_RETRIEVAL_SCORING_WAL_IMPORTANCE", &cfg.Retrieval.Scoring.WAL.Importance)
	overrides.float64("PALI_RETRIEVAL_SCORING_MATCH_RECENCY", &cfg.Retrieval.Scoring.Match.Recency)
	overrides.float64("PALI_RETRIEVAL_SCORING_MATCH_RELEVANCE", &cfg.Retrieval.Scoring.Match.Relevance)
	overrides.float64("PALI_RETRIEVAL_SCORING_MATCH_IMPORTANCE", &cfg.Retrieval.Scoring.Match.Importance)
	overrides.float64("PALI_RETRIEVAL_SCORING_MATCH_QUERY_OVERLAP", &cfg.Retrieval.Scoring.Match.QueryOverlap)
	overrides.float64("PALI_RETRIEVAL_SCORING_MATCH_ROUTING", &cfg.Retrieval.Scoring.Match.Routing)

	overrides.bool("PALI_RETRIEVAL_MULTI_HOP_ENTITY_FACT_BRIDGE_ENABLED", &cfg.Retrieval.MultiHop.EntityFactBridgeEnabled)
	overrides.bool("PALI_RETRIEVAL_MULTI_HOP_LLM_DECOMPOSITION_ENABLED", &cfg.Retrieval.MultiHop.LLMDecompositionEnabled)
	overrides.string("PALI_RETRIEVAL_MULTI_HOP_DECOMPOSITION_PROVIDER", &cfg.Retrieval.MultiHop.DecompositionProvider)
	overrides.string("PALI_RETRIEVAL_MULTI_HOP_OPENROUTER_MODEL", &cfg.Retrieval.MultiHop.OpenRouterModel)
	overrides.string("PALI_RETRIEVAL_MULTI_HOP_OLLAMA_BASE_URL", &cfg.Retrieval.MultiHop.OllamaBaseURL)
	overrides.string("PALI_RETRIEVAL_MULTI_HOP_OLLAMA_MODEL", &cfg.Retrieval.MultiHop.OllamaModel)
	overrides.int("PALI_RETRIEVAL_MULTI_HOP_OLLAMA_TIMEOUT_MS", &cfg.Retrieval.MultiHop.OllamaTimeoutMS)
	overrides.int("PALI_RETRIEVAL_MULTI_HOP_MAX_DECOMPOSITION_QUERIES", &cfg.Retrieval.MultiHop.MaxDecompositionQueries)
	overrides.bool("PALI_RETRIEVAL_MULTI_HOP_ENABLE_PAIRWISE_RERANK", &cfg.Retrieval.MultiHop.EnablePairwiseRerank)
	overrides.bool("PALI_RETRIEVAL_MULTI_HOP_TOKEN_EXPANSION_FALLBACK", &cfg.Retrieval.MultiHop.TokenExpansionFallback)
	overrides.bool("PALI_RETRIEVAL_MULTI_HOP_GRAPH_PATH_ENABLED", &cfg.Retrieval.MultiHop.GraphPathEnabled)
	overrides.int("PALI_RETRIEVAL_MULTI_HOP_GRAPH_MAX_HOPS", &cfg.Retrieval.MultiHop.GraphMaxHops)
	overrides.int("PALI_RETRIEVAL_MULTI_HOP_GRAPH_SEED_LIMIT", &cfg.Retrieval.MultiHop.GraphSeedLimit)
	overrides.int("PALI_RETRIEVAL_MULTI_HOP_GRAPH_PATH_LIMIT", &cfg.Retrieval.MultiHop.GraphPathLimit)
	overrides.float64("PALI_RETRIEVAL_MULTI_HOP_GRAPH_MIN_SCORE", &cfg.Retrieval.MultiHop.GraphMinScore)
	overrides.float64("PALI_RETRIEVAL_MULTI_HOP_GRAPH_WEIGHT", &cfg.Retrieval.MultiHop.GraphWeight)
	overrides.bool("PALI_RETRIEVAL_MULTI_HOP_GRAPH_TEMPORAL_VALIDITY", &cfg.Retrieval.MultiHop.GraphTemporalValidity)
	overrides.bool("PALI_RETRIEVAL_MULTI_HOP_GRAPH_SINGLETON_INVALIDATION", &cfg.Retrieval.MultiHop.GraphSingletonInvalidation)

	overrides.bool("PALI_PARSER_ENABLED", &cfg.Parser.Enabled)
	overrides.string("PALI_PARSER_PROVIDER", &cfg.Parser.Provider)
	overrides.string("PALI_PARSER_OLLAMA_BASE_URL", &cfg.Parser.OllamaBaseURL)
	overrides.string("PALI_PARSER_OLLAMA_MODEL", &cfg.Parser.OllamaModel)
	overrides.string("PALI_PARSER_OPENROUTER_MODEL", &cfg.Parser.OpenRouterModel)
	overrides.int("PALI_PARSER_OLLAMA_TIMEOUT_MS", &cfg.Parser.OllamaTimeoutMS)
	overrides.bool("PALI_PARSER_STORE_RAW_TURN", &cfg.Parser.StoreRawTurn)
	overrides.int("PALI_PARSER_MAX_FACTS", &cfg.Parser.MaxFacts)
	overrides.float64("PALI_PARSER_DEDUPE_THRESHOLD", &cfg.Parser.DedupeThreshold)
	overrides.float64("PALI_PARSER_UPDATE_THRESHOLD", &cfg.Parser.UpdateThreshold)
	overrides.bool("PALI_PARSER_ANSWER_SPAN_RETENTION_ENABLED", &cfg.Parser.AnswerSpanRetentionEnabled)

	overrides.bool("PALI_PROFILE_LAYER_SUPPORT_LINKS_ENABLED", &cfg.ProfileLayer.SupportLinksEnabled)

	overrides.string("PALI_DATABASE_SQLITE_DSN", &cfg.Database.SQLiteDSN)

	overrides.string("PALI_QDRANT_BASE_URL", &cfg.Qdrant.BaseURL)
	overrides.string("PALI_QDRANT_API_KEY", &cfg.Qdrant.APIKey)
	overrides.string("PALI_QDRANT_COLLECTION", &cfg.Qdrant.Collection)
	overrides.int("PALI_QDRANT_TIMEOUT_MS", &cfg.Qdrant.TimeoutMS)

	overrides.string("PALI_PGVECTOR_DSN", &cfg.PGVector.DSN)
	overrides.string("PALI_PGVECTOR_TABLE", &cfg.PGVector.Table)
	overrides.bool("PALI_PGVECTOR_AUTO_MIGRATE", &cfg.PGVector.AutoMigrate)
	overrides.int("PALI_PGVECTOR_MAX_OPEN_CONNS", &cfg.PGVector.MaxOpenConns)
	overrides.int("PALI_PGVECTOR_MAX_IDLE_CONNS", &cfg.PGVector.MaxIdleConns)

	overrides.string("PALI_NEO4J_URI", &cfg.Neo4j.URI)
	overrides.string("PALI_NEO4J_USERNAME", &cfg.Neo4j.Username)
	overrides.string("PALI_NEO4J_PASSWORD", &cfg.Neo4j.Password)
	overrides.string("PALI_NEO4J_DATABASE", &cfg.Neo4j.Database)
	overrides.int("PALI_NEO4J_TIMEOUT_MS", &cfg.Neo4j.TimeoutMS)
	overrides.int("PALI_NEO4J_BATCH_SIZE", &cfg.Neo4j.BatchSize)

	overrides.string("PALI_EMBEDDING_PROVIDER", &cfg.Embedding.Provider)
	overrides.string("PALI_EMBEDDING_FALLBACK_PROVIDER", &cfg.Embedding.FallbackProvider)
	overrides.string("PALI_EMBEDDING_MODEL_PATH", &cfg.Embedding.ModelPath)
	overrides.string("PALI_EMBEDDING_TOKENIZER_PATH", &cfg.Embedding.TokenizerPath)
	overrides.string("PALI_EMBEDDING_OLLAMA_BASE_URL", &cfg.Embedding.OllamaBaseURL)
	overrides.string("PALI_EMBEDDING_OLLAMA_MODEL", &cfg.Embedding.OllamaModel)
	overrides.int("PALI_EMBEDDING_OLLAMA_TIMEOUT_SECONDS", &cfg.Embedding.OllamaTimeoutSeconds)

	overrides.string("PALI_OPENROUTER_BASE_URL", &cfg.OpenRouter.BaseURL)
	overrides.string("PALI_OPENROUTER_API_KEY", &cfg.OpenRouter.APIKey)
	overrides.string("PALI_OPENROUTER_EMBEDDING_MODEL", &cfg.OpenRouter.EmbeddingModel)
	overrides.string("PALI_OPENROUTER_SCORING_MODEL", &cfg.OpenRouter.ScoringModel)
	overrides.int("PALI_OPENROUTER_TIMEOUT_MS", &cfg.OpenRouter.TimeoutMS)

	overrides.string("PALI_OLLAMA_BASE_URL", &cfg.Ollama.BaseURL)
	overrides.string("PALI_OLLAMA_MODEL", &cfg.Ollama.Model)
	overrides.int("PALI_OLLAMA_TIMEOUT_MS", &cfg.Ollama.TimeoutMS)

	overrides.bool("PALI_AUTH_ENABLED", &cfg.Auth.Enabled)
	overrides.string("PALI_AUTH_JWT_SECRET", &cfg.Auth.JWTSecret)
	overrides.string("PALI_AUTH_ISSUER", &cfg.Auth.Issuer)

	overrides.bool("PALI_LOGGING_DEV_VERBOSE", &cfg.Logging.DevVerbose)
	overrides.bool("PALI_LOGGING_PROGRESS", &cfg.Logging.Progress)

	return overrides.err()
}

func applyLegacyEnvironmentFallbacks(cfg *Config) {
	if strings.TrimSpace(cfg.OpenRouter.APIKey) == "" {
		cfg.OpenRouter.APIKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	if strings.TrimSpace(cfg.Neo4j.Password) == "" {
		cfg.Neo4j.Password = strings.TrimSpace(os.Getenv("NEO4J_PASSWORD"))
	}
}

type envOverrides struct {
	errs []string
}

func (o *envOverrides) string(name string, target *string) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return
	}
	*target = strings.TrimSpace(value)
}

func (o *envOverrides) int(name string, target *int) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		o.errs = append(o.errs, fmt.Sprintf("%s=%q must be an integer", name, value))
		return
	}
	*target = parsed
}

func (o *envOverrides) bool(name string, target *bool) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		o.errs = append(o.errs, fmt.Sprintf("%s=%q must be a boolean", name, value))
		return
	}
	*target = parsed
}

func (o *envOverrides) float64(name string, target *float64) {
	value, ok := os.LookupEnv(name)
	if !ok {
		return
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		o.errs = append(o.errs, fmt.Sprintf("%s=%q must be a number", name, value))
		return
	}
	*target = parsed
}

func (o *envOverrides) err() error {
	if len(o.errs) == 0 {
		return nil
	}
	return fmt.Errorf("invalid environment override(s): %s", strings.Join(o.errs, "; "))
}
