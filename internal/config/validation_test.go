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

	cfg.Embedding.FallbackProvider = "openrouter"
	cfg.OpenRouter.APIKey = "test-key"
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

	cfg.ImportanceScorer = "openrouter"
	cfg.OpenRouter.APIKey = "test-key"
	cfg.OpenRouter.ScoringModel = "openai/gpt-oss-120b:nitro"
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

func TestValidate_OpenRouterEmbedderRequiresKey(t *testing.T) {
	cfg := Defaults()
	cfg.Embedding.Provider = "openrouter"
	cfg.OpenRouter.APIKey = ""

	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "openrouter.api_key")
}

func TestValidate_OpenRouterScorerRequiresModel(t *testing.T) {
	cfg := Defaults()
	cfg.ImportanceScorer = "openrouter"
	cfg.OpenRouter.APIKey = "test-key"
	cfg.OpenRouter.ScoringModel = ""

	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "openrouter.scoring_model")
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

func TestValidate_PGVectorBackendRequiresValidConfig(t *testing.T) {
	cfg := Defaults()
	cfg.VectorBackend = "pgvector"
	cfg.PGVector.DSN = "postgres://user:pass@localhost:5432/pali"
	require.NoError(t, Validate(cfg))

	cfg = Defaults()
	cfg.VectorBackend = "pgvector"
	cfg.PGVector.DSN = ""
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pgvector.dsn")

	cfg = Defaults()
	cfg.VectorBackend = "pgvector"
	cfg.PGVector.DSN = "://bad"
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pgvector.dsn")

	cfg = Defaults()
	cfg.VectorBackend = "pgvector"
	cfg.PGVector.DSN = "postgres://user:pass@localhost:5432/pali"
	cfg.PGVector.Table = ""
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pgvector.table")

	cfg = Defaults()
	cfg.VectorBackend = "pgvector"
	cfg.PGVector.DSN = "postgres://user:pass@localhost:5432/pali"
	cfg.PGVector.MaxOpenConns = -1
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pgvector.max_open_conns")
}

func TestValidate_EntityFactBackendRequiresValidConfig(t *testing.T) {
	cfg := Defaults()
	cfg.EntityFactBackend = "neo4j"
	cfg.Neo4j.Password = "secret"
	require.NoError(t, Validate(cfg))

	cfg = Defaults()
	cfg.EntityFactBackend = "neo4j"
	cfg.Neo4j.Password = "secret"
	cfg.Neo4j.URI = "://bad"
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "neo4j.uri")

	cfg = Defaults()
	cfg.EntityFactBackend = "neo4j"
	cfg.Neo4j.Password = ""
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "neo4j.password")

	cfg = Defaults()
	cfg.EntityFactBackend = "neo4j"
	cfg.Neo4j.Password = "secret"
	cfg.Neo4j.BatchSize = 0
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "neo4j.batch_size")
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

func TestValidate_PostprocessOptions(t *testing.T) {
	cfg := Defaults()
	cfg.Postprocess.PollIntervalMS = 0
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "postprocess.poll_interval_ms")

	cfg = Defaults()
	cfg.Postprocess.WorkerCount = 0
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "postprocess.worker_count")

	cfg = Defaults()
	cfg.Postprocess.RetryBaseMS = 70000
	cfg.Postprocess.RetryMaxMS = 60000
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "postprocess.retry_base_ms")
}

func TestValidate_MultiHopGraphOptions(t *testing.T) {
	cfg := Defaults()
	cfg.Retrieval.MultiHop.GraphMaxHops = 0
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.multi_hop.graph_max_hops")

	cfg = Defaults()
	cfg.Retrieval.MultiHop.GraphSeedLimit = 0
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.multi_hop.graph_seed_limit")

	cfg = Defaults()
	cfg.Retrieval.MultiHop.GraphPathLimit = 0
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.multi_hop.graph_path_limit")

	cfg = Defaults()
	cfg.Retrieval.MultiHop.GraphMinScore = 1.1
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.multi_hop.graph_min_score")

	cfg = Defaults()
	cfg.Retrieval.MultiHop.GraphWeight = -0.1
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.multi_hop.graph_weight")
}

func TestValidate_RetrievalSearchTuningOptions(t *testing.T) {
	cfg := Defaults()
	cfg.Retrieval.Search.AdaptiveQueryMaxExtraQueries = 0
	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.search.adaptive_query_max_extra_queries")

	cfg = Defaults()
	cfg.Retrieval.Search.AdaptiveQueryWeakLexicalThreshold = 1.1
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.search.adaptive_query_weak_lexical_threshold")

	cfg = Defaults()
	cfg.Retrieval.Search.CandidateWindowMin = 128
	cfg.Retrieval.Search.CandidateWindowMax = 64
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.search.candidate_window_max")

	cfg = Defaults()
	cfg.Retrieval.Search.EarlyRerankBaseWindow = 0
	err = Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "retrieval.search.early_rerank_base_window")
}

func TestValidate_CategoryImprovementFlagsAreAdditive(t *testing.T) {
	cfg := Defaults()
	cfg.Retrieval.AnswerTypeRoutingEnabled = true
	cfg.Retrieval.EarlyRankRerankEnabled = true
	cfg.Retrieval.TemporalResolverEnabled = true
	cfg.Retrieval.OpenDomainAlternativeResolverEnabled = true
	cfg.Parser.AnswerSpanRetentionEnabled = true
	cfg.ProfileLayer.SupportLinksEnabled = true
	require.NoError(t, Validate(cfg))
}
