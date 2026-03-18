package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server            ServerConfig           `yaml:"server"`
	VectorBackend     string                 `yaml:"vector_backend"`
	EntityFactBackend string                 `yaml:"entity_fact_backend"`
	DefaultTenantID   string                 `yaml:"default_tenant_id"`
	ImportanceScorer  string                 `yaml:"importance_scorer"`
	Postprocess       PostprocessConfig      `yaml:"postprocess"`
	StructuredMemory  StructuredMemoryConfig `yaml:"structured_memory"`
	Retrieval         RetrievalConfig        `yaml:"retrieval"`
	Parser            ParserConfig           `yaml:"parser"`
	ProfileLayer      ProfileLayerConfig     `yaml:"profile_layer"`
	Database          Database               `yaml:"database"`
	Qdrant            QdrantConfig           `yaml:"qdrant"`
	Neo4j             Neo4jConfig            `yaml:"neo4j"`
	Embedding         Embedding              `yaml:"embedding"`
	OpenRouter        OpenRouterConfig       `yaml:"openrouter"`
	Ollama            OllamaConfig           `yaml:"ollama"`
	Auth              AuthConfig             `yaml:"auth"`
	Logging           LoggingConfig          `yaml:"logging"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

type Embedding struct {
	Provider             string `yaml:"provider"`
	FallbackProvider     string `yaml:"fallback_provider"`
	ModelPath            string `yaml:"model_path"`
	TokenizerPath        string `yaml:"tokenizer_path"`
	OllamaBaseURL        string `yaml:"ollama_base_url"`
	OllamaModel          string `yaml:"ollama_model"`
	OllamaTimeoutSeconds int    `yaml:"ollama_timeout_seconds"`
}

type OllamaConfig struct {
	BaseURL   string `yaml:"base_url"`
	Model     string `yaml:"model"`
	TimeoutMS int    `yaml:"timeout_ms"`
}

type OpenRouterConfig struct {
	BaseURL        string `yaml:"base_url"`
	APIKey         string `yaml:"api_key"`
	EmbeddingModel string `yaml:"embedding_model"`
	ScoringModel   string `yaml:"scoring_model"`
	TimeoutMS      int    `yaml:"timeout_ms"`
}

type QdrantConfig struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	Collection string `yaml:"collection"`
	TimeoutMS  int    `yaml:"timeout_ms"`
}

type Neo4jConfig struct {
	URI       string `yaml:"uri"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
	Database  string `yaml:"database"`
	TimeoutMS int    `yaml:"timeout_ms"`
	BatchSize int    `yaml:"batch_size"`
}

type AuthConfig struct {
	Enabled   bool   `yaml:"enabled"`
	JWTSecret string `yaml:"jwt_secret"`
	Issuer    string `yaml:"issuer"`
}

type Database struct {
	SQLiteDSN string `yaml:"sqlite_dsn"`
}

type StructuredMemoryConfig struct {
	Enabled               bool `yaml:"enabled"`
	DualWriteObservations bool `yaml:"dual_write_observations"`
	DualWriteEvents       bool `yaml:"dual_write_events"`
	MaxObservations       int  `yaml:"max_observations"`
}

type PostprocessConfig struct {
	Enabled        bool `yaml:"enabled"`
	PollIntervalMS int  `yaml:"poll_interval_ms"`
	BatchSize      int  `yaml:"batch_size"`
	WorkerCount    int  `yaml:"worker_count"`
	LeaseMS        int  `yaml:"lease_ms"`
	MaxAttempts    int  `yaml:"max_attempts"`
	RetryBaseMS    int  `yaml:"retry_base_ms"`
	RetryMaxMS     int  `yaml:"retry_max_ms"`
}

type RetrievalConfig struct {
	Scoring                              RetrievalScoringConfig  `yaml:"scoring"`
	Search                               RetrievalSearchConfig   `yaml:"search"`
	MultiHop                             RetrievalMultiHopConfig `yaml:"multi_hop"`
	AnswerTypeRoutingEnabled             bool                    `yaml:"answer_type_routing_enabled"`
	EarlyRankRerankEnabled               bool                    `yaml:"early_rank_rerank_enabled"`
	TemporalResolverEnabled              bool                    `yaml:"temporal_resolver_enabled"`
	OpenDomainAlternativeResolverEnabled bool                    `yaml:"open_domain_alternative_resolver_enabled"`
}

type RetrievalSearchConfig struct {
	AdaptiveQueryExpansionEnabled        bool    `yaml:"adaptive_query_expansion_enabled"`
	AdaptiveQueryMaxExtraQueries         int     `yaml:"adaptive_query_max_extra_queries"`
	AdaptiveQueryWeakLexicalThreshold    float64 `yaml:"adaptive_query_weak_lexical_threshold"`
	AdaptiveQueryPlanConfidenceThreshold float64 `yaml:"adaptive_query_plan_confidence_threshold"`
	CandidateWindowMultiplier            int     `yaml:"candidate_window_multiplier"`
	CandidateWindowMin                   int     `yaml:"candidate_window_min"`
	CandidateWindowMax                   int     `yaml:"candidate_window_max"`
	CandidateWindowTemporalBoost         int     `yaml:"candidate_window_temporal_boost"`
	CandidateWindowMultiHopBoost         int     `yaml:"candidate_window_multi_hop_boost"`
	CandidateWindowFilterBoost           int     `yaml:"candidate_window_filter_boost"`
	EarlyRerankBaseWindow                int     `yaml:"early_rerank_base_window"`
	EarlyRerankMaxWindow                 int     `yaml:"early_rerank_max_window"`
}

type RetrievalScoringConfig struct {
	Algorithm string                    `yaml:"algorithm"`
	WAL       ScoringWeightsConfig      `yaml:"wal"`
	Match     MatchScoringWeightsConfig `yaml:"match"`
}

type RetrievalMultiHopConfig struct {
	EntityFactBridgeEnabled    bool    `yaml:"entity_fact_bridge_enabled"`
	LLMDecompositionEnabled    bool    `yaml:"llm_decomposition_enabled"`
	DecompositionProvider      string  `yaml:"decomposition_provider"`
	OpenRouterModel            string  `yaml:"openrouter_model"`
	OllamaBaseURL              string  `yaml:"ollama_base_url"`
	OllamaModel                string  `yaml:"ollama_model"`
	OllamaTimeoutMS            int     `yaml:"ollama_timeout_ms"`
	MaxDecompositionQueries    int     `yaml:"max_decomposition_queries"`
	EnablePairwiseRerank       bool    `yaml:"enable_pairwise_rerank"`
	TokenExpansionFallback     bool    `yaml:"token_expansion_fallback"`
	GraphPathEnabled           bool    `yaml:"graph_path_enabled"`
	GraphMaxHops               int     `yaml:"graph_max_hops"`
	GraphSeedLimit             int     `yaml:"graph_seed_limit"`
	GraphPathLimit             int     `yaml:"graph_path_limit"`
	GraphMinScore              float64 `yaml:"graph_min_score"`
	GraphWeight                float64 `yaml:"graph_weight"`
	GraphTemporalValidity      bool    `yaml:"graph_temporal_validity"`
	GraphSingletonInvalidation bool    `yaml:"graph_singleton_invalidation"`
}

type ScoringWeightsConfig struct {
	Recency    float64 `yaml:"recency"`
	Relevance  float64 `yaml:"relevance"`
	Importance float64 `yaml:"importance"`
}

type MatchScoringWeightsConfig struct {
	Recency      float64 `yaml:"recency"`
	Relevance    float64 `yaml:"relevance"`
	Importance   float64 `yaml:"importance"`
	QueryOverlap float64 `yaml:"query_overlap"`
	Routing      float64 `yaml:"routing"`
}

type ParserConfig struct {
	Enabled                    bool    `yaml:"enabled"`
	Provider                   string  `yaml:"provider"`
	OllamaBaseURL              string  `yaml:"ollama_base_url"`
	OllamaModel                string  `yaml:"ollama_model"`
	OpenRouterModel            string  `yaml:"openrouter_model"`
	OllamaTimeoutMS            int     `yaml:"ollama_timeout_ms"`
	StoreRawTurn               bool    `yaml:"store_raw_turn"`
	MaxFacts                   int     `yaml:"max_facts"`
	DedupeThreshold            float64 `yaml:"dedupe_threshold"`
	UpdateThreshold            float64 `yaml:"update_threshold"`
	AnswerSpanRetentionEnabled bool    `yaml:"answer_span_retention_enabled"`
}

type ProfileLayerConfig struct {
	SupportLinksEnabled bool `yaml:"support_links_enabled"`
}

type LoggingConfig struct {
	DevVerbose bool `yaml:"dev_verbose"`
	Progress   bool `yaml:"progress"`
}

func Load(path string) (Config, error) {
	cfg := Defaults()

	if path == "" {
		applyEnvironment(&cfg)
		return cfg, nil
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	applyEnvironment(&cfg)
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyEnvironment(cfg *Config) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.OpenRouter.APIKey) == "" {
		cfg.OpenRouter.APIKey = strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	}
	if strings.TrimSpace(cfg.Neo4j.Password) == "" {
		cfg.Neo4j.Password = strings.TrimSpace(os.Getenv("NEO4J_PASSWORD"))
	}
}
