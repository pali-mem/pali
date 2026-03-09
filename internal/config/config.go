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
	Scoring  RetrievalScoringConfig  `yaml:"scoring"`
	MultiHop RetrievalMultiHopConfig `yaml:"multi_hop"`
}

type RetrievalScoringConfig struct {
	Algorithm string                    `yaml:"algorithm"`
	WAL       ScoringWeightsConfig      `yaml:"wal"`
	Match     MatchScoringWeightsConfig `yaml:"match"`
}

type RetrievalMultiHopConfig struct {
	EntityFactBridgeEnabled bool   `yaml:"entity_fact_bridge_enabled"`
	LLMDecompositionEnabled bool   `yaml:"llm_decomposition_enabled"`
	DecompositionProvider   string `yaml:"decomposition_provider"`
	OpenRouterModel         string `yaml:"openrouter_model"`
	OllamaBaseURL           string `yaml:"ollama_base_url"`
	OllamaModel             string `yaml:"ollama_model"`
	OllamaTimeoutMS         int    `yaml:"ollama_timeout_ms"`
	MaxDecompositionQueries int    `yaml:"max_decomposition_queries"`
	EnablePairwiseRerank    bool   `yaml:"enable_pairwise_rerank"`
	TokenExpansionFallback  bool   `yaml:"token_expansion_fallback"`
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
	Enabled         bool    `yaml:"enabled"`
	Provider        string  `yaml:"provider"`
	OllamaBaseURL   string  `yaml:"ollama_base_url"`
	OllamaModel     string  `yaml:"ollama_model"`
	OpenRouterModel string  `yaml:"openrouter_model"`
	OllamaTimeoutMS int     `yaml:"ollama_timeout_ms"`
	StoreRawTurn    bool    `yaml:"store_raw_turn"`
	MaxFacts        int     `yaml:"max_facts"`
	DedupeThreshold float64 `yaml:"dedupe_threshold"`
	UpdateThreshold float64 `yaml:"update_threshold"`
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
