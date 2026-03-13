package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	require.Equal(t, 8080, cfg.Server.Port)
	require.Equal(t, "sqlite", cfg.VectorBackend)
	require.Equal(t, "sqlite", cfg.EntityFactBackend)
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
	require.True(t, cfg.Retrieval.AnswerTypeRoutingEnabled)
	require.True(t, cfg.Retrieval.EarlyRankRerankEnabled)
	require.True(t, cfg.Retrieval.TemporalResolverEnabled)
	require.False(t, cfg.Retrieval.OpenDomainAlternativeResolverEnabled)
	require.Equal(t, 0.1, cfg.Retrieval.Scoring.WAL.Recency)
	require.Equal(t, 0.8, cfg.Retrieval.Scoring.WAL.Relevance)
	require.Equal(t, 0.1, cfg.Retrieval.Scoring.WAL.Importance)
	require.False(t, cfg.Retrieval.Search.AdaptiveQueryExpansionEnabled)
	require.Equal(t, 2, cfg.Retrieval.Search.AdaptiveQueryMaxExtraQueries)
	require.Equal(t, 0.62, cfg.Retrieval.Search.AdaptiveQueryWeakLexicalThreshold)
	require.Equal(t, 0.0, cfg.Retrieval.Search.AdaptiveQueryPlanConfidenceThreshold)
	require.Equal(t, 5, cfg.Retrieval.Search.CandidateWindowMultiplier)
	require.Equal(t, 50, cfg.Retrieval.Search.CandidateWindowMin)
	require.Equal(t, 200, cfg.Retrieval.Search.CandidateWindowMax)
	require.Equal(t, 40, cfg.Retrieval.Search.CandidateWindowTemporalBoost)
	require.Equal(t, 80, cfg.Retrieval.Search.CandidateWindowMultiHopBoost)
	require.Equal(t, 30, cfg.Retrieval.Search.CandidateWindowFilterBoost)
	require.Equal(t, 25, cfg.Retrieval.Search.EarlyRerankBaseWindow)
	require.Equal(t, 25, cfg.Retrieval.Search.EarlyRerankMaxWindow)
	require.True(t, cfg.Retrieval.MultiHop.EntityFactBridgeEnabled)
	require.False(t, cfg.Retrieval.MultiHop.LLMDecompositionEnabled)
	require.Equal(t, "openrouter", cfg.Retrieval.MultiHop.DecompositionProvider)
	require.Equal(t, "openai/gpt-oss-120b:nitro", cfg.Retrieval.MultiHop.OpenRouterModel)
	require.Equal(t, 3, cfg.Retrieval.MultiHop.MaxDecompositionQueries)
	require.True(t, cfg.Retrieval.MultiHop.EnablePairwiseRerank)
	require.True(t, cfg.Retrieval.MultiHop.TokenExpansionFallback)
	require.False(t, cfg.Retrieval.MultiHop.GraphPathEnabled)
	require.Equal(t, 2, cfg.Retrieval.MultiHop.GraphMaxHops)
	require.Equal(t, 12, cfg.Retrieval.MultiHop.GraphSeedLimit)
	require.Equal(t, 128, cfg.Retrieval.MultiHop.GraphPathLimit)
	require.Equal(t, 0.12, cfg.Retrieval.MultiHop.GraphMinScore)
	require.Equal(t, 0.25, cfg.Retrieval.MultiHop.GraphWeight)
	require.False(t, cfg.Retrieval.MultiHop.GraphTemporalValidity)
	require.True(t, cfg.Retrieval.MultiHop.GraphSingletonInvalidation)
	require.False(t, cfg.Parser.Enabled)
	require.Equal(t, "heuristic", cfg.Parser.Provider)
	require.Equal(t, 4, cfg.Parser.MaxFacts)
	require.False(t, cfg.Parser.AnswerSpanRetentionEnabled)
	require.False(t, cfg.ProfileLayer.SupportLinksEnabled)
	require.Equal(t, "lexical", cfg.Embedding.Provider)
	require.Equal(t, "lexical", cfg.Embedding.FallbackProvider)
	require.Equal(t, "mxbai-embed-large", cfg.Embedding.OllamaModel)
	require.Equal(t, "https://openrouter.ai/api/v1", cfg.OpenRouter.BaseURL)
	require.Equal(t, "openai/text-embedding-3-small:nitro", cfg.OpenRouter.EmbeddingModel)
	require.Equal(t, "openai/gpt-oss-120b:nitro", cfg.OpenRouter.ScoringModel)
	require.Equal(t, 10000, cfg.OpenRouter.TimeoutMS)
	require.Equal(t, "http://127.0.0.1:6333", cfg.Qdrant.BaseURL)
	require.Equal(t, "pali_memories", cfg.Qdrant.Collection)
	require.Equal(t, 2000, cfg.Qdrant.TimeoutMS)
	require.Equal(t, "bolt://127.0.0.1:7687", cfg.Neo4j.URI)
	require.Equal(t, "neo4j", cfg.Neo4j.Username)
	require.Equal(t, "neo4j", cfg.Neo4j.Database)
	require.Equal(t, 2000, cfg.Neo4j.TimeoutMS)
	require.Equal(t, 256, cfg.Neo4j.BatchSize)
	require.Equal(t, "http://127.0.0.1:11434", cfg.Ollama.BaseURL)
	require.Equal(t, "deepseek-r1:7b", cfg.Ollama.Model)
	require.Equal(t, 2000, cfg.Ollama.TimeoutMS)
}

func TestLoad_OpenRouterAPIKeyFromEnvWhenMissingInYAML(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "env-key")
	dir := t.TempDir()
	path := filepath.Join(dir, "pali.yaml")
	require.NoError(t, os.WriteFile(path, []byte("embedding:\n  provider: openrouter\n"), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "env-key", cfg.OpenRouter.APIKey)
}

func TestLoad_OpenRouterYAMLKeyTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "env-key")
	dir := t.TempDir()
	path := filepath.Join(dir, "pali.yaml")
	require.NoError(t, os.WriteFile(path, []byte("openrouter:\n  api_key: yaml-key\n"), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "yaml-key", cfg.OpenRouter.APIKey)
}

func TestLoad_Neo4jPasswordFromEnvWhenMissingInYAML(t *testing.T) {
	t.Setenv("NEO4J_PASSWORD", "env-pass")
	dir := t.TempDir()
	path := filepath.Join(dir, "pali.yaml")
	require.NoError(t, os.WriteFile(path, []byte("entity_fact_backend: neo4j\nneo4j:\n  username: neo4j\n"), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "env-pass", cfg.Neo4j.Password)
}

func TestLoad_Neo4jYAMLPasswordTakesPrecedenceOverEnv(t *testing.T) {
	t.Setenv("NEO4J_PASSWORD", "env-pass")
	dir := t.TempDir()
	path := filepath.Join(dir, "pali.yaml")
	require.NoError(t, os.WriteFile(path, []byte("neo4j:\n  password: yaml-pass\n"), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "yaml-pass", cfg.Neo4j.Password)
}
