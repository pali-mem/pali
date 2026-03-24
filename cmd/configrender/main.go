package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pali-mem/pali/internal/config"
	"gopkg.in/yaml.v3"
)

func main() {
	var (
		profilePath string
		outPath     string

		host string
		port int

		vectorBackend     string
		entityFactBackend string
		sqliteDSN         string

		qdrantURL        string
		qdrantAPIKey     string
		qdrantCollection string
		qdrantTimeoutMS  int

		pgvectorDSN          string
		pgvectorTable        string
		pgvectorAutoMigrate  string
		pgvectorMaxOpenConns int
		pgvectorMaxIdleConns int

		embeddingProvider       string
		embeddingFallback       string
		embeddingOllamaURL      string
		embeddingOllamaModel    string
		embeddingOllamaTimeoutS int
		embeddingModelPath      string
		embeddingTokenizerPath  string

		parserEnabled         string
		parserProvider        string
		parserOllamaModel     string
		parserOpenRouterModel string
	)

	flag.StringVar(&profilePath, "profile", "", "Path to base YAML profile")
	flag.StringVar(&outPath, "out", "", "Output YAML path")

	flag.StringVar(&host, "host", "", "Override server.host")
	flag.IntVar(&port, "port", 0, "Override server.port")

	flag.StringVar(&vectorBackend, "vector-backend", "", "Override vector_backend")
	flag.StringVar(&entityFactBackend, "entity-fact-backend", "", "Override entity_fact_backend")
	flag.StringVar(&sqliteDSN, "sqlite-dsn", "", "Override database.sqlite_dsn")

	flag.StringVar(&qdrantURL, "qdrant-url", "", "Override qdrant.base_url")
	flag.StringVar(&qdrantAPIKey, "qdrant-api-key", "", "Override qdrant.api_key")
	flag.StringVar(&qdrantCollection, "qdrant-collection", "", "Override qdrant.collection")
	flag.IntVar(&qdrantTimeoutMS, "qdrant-timeout-ms", -1, "Override qdrant.timeout_ms")
	flag.StringVar(&pgvectorDSN, "pgvector-dsn", "", "Override pgvector.dsn")
	flag.StringVar(&pgvectorTable, "pgvector-table", "", "Override pgvector.table")
	flag.StringVar(&pgvectorAutoMigrate, "pgvector-auto-migrate", "", "Override pgvector.auto_migrate (true|false)")
	flag.IntVar(&pgvectorMaxOpenConns, "pgvector-max-open-conns", -1, "Override pgvector.max_open_conns")
	flag.IntVar(&pgvectorMaxIdleConns, "pgvector-max-idle-conns", -1, "Override pgvector.max_idle_conns")

	flag.StringVar(&embeddingProvider, "embedding-provider", "", "Override embedding.provider")
	flag.StringVar(&embeddingFallback, "embedding-fallback-provider", "", "Override embedding.fallback_provider")
	flag.StringVar(&embeddingOllamaURL, "embedding-ollama-url", "", "Override embedding.ollama_base_url")
	flag.StringVar(&embeddingOllamaModel, "embedding-ollama-model", "", "Override embedding.ollama_model")
	flag.IntVar(&embeddingOllamaTimeoutS, "embedding-ollama-timeout-seconds", -1, "Override embedding.ollama_timeout_seconds")
	flag.StringVar(&embeddingModelPath, "embedding-model-path", "", "Override embedding.model_path")
	flag.StringVar(&embeddingTokenizerPath, "embedding-tokenizer-path", "", "Override embedding.tokenizer_path")
	flag.StringVar(&parserEnabled, "parser-enabled", "", "Override parser.enabled (true|false)")
	flag.StringVar(&parserProvider, "parser-provider", "", "Override parser.provider")
	flag.StringVar(&parserOllamaModel, "parser-ollama-model", "", "Override parser.ollama_model")
	flag.StringVar(&parserOpenRouterModel, "parser-openrouter-model", "", "Override parser.openrouter_model")

	flag.Parse()

	if strings.TrimSpace(profilePath) == "" || strings.TrimSpace(outPath) == "" {
		exitf("both -profile and -out are required")
	}

	cfg, err := config.Load(profilePath)
	if err != nil {
		exitf("load profile %q: %v", profilePath, err)
	}

	if strings.TrimSpace(host) != "" {
		cfg.Server.Host = strings.TrimSpace(host)
	}
	if port > 0 {
		cfg.Server.Port = port
	}
	if strings.TrimSpace(vectorBackend) != "" {
		cfg.VectorBackend = strings.ToLower(strings.TrimSpace(vectorBackend))
	}
	if strings.TrimSpace(entityFactBackend) != "" {
		cfg.EntityFactBackend = strings.ToLower(strings.TrimSpace(entityFactBackend))
	}
	if strings.TrimSpace(sqliteDSN) != "" {
		cfg.Database.SQLiteDSN = strings.TrimSpace(sqliteDSN)
	}
	if strings.TrimSpace(qdrantURL) != "" {
		cfg.Qdrant.BaseURL = strings.TrimSpace(qdrantURL)
	}
	if qdrantAPIKey != "" {
		cfg.Qdrant.APIKey = qdrantAPIKey
	}
	if strings.TrimSpace(qdrantCollection) != "" {
		cfg.Qdrant.Collection = strings.TrimSpace(qdrantCollection)
	}
	if qdrantTimeoutMS >= 0 {
		cfg.Qdrant.TimeoutMS = qdrantTimeoutMS
	}
	if strings.TrimSpace(pgvectorDSN) != "" {
		cfg.PGVector.DSN = strings.TrimSpace(pgvectorDSN)
	}
	if strings.TrimSpace(pgvectorTable) != "" {
		cfg.PGVector.Table = strings.TrimSpace(pgvectorTable)
	}
	if strings.TrimSpace(pgvectorAutoMigrate) != "" {
		enabled, parseErr := strconv.ParseBool(strings.TrimSpace(pgvectorAutoMigrate))
		if parseErr != nil {
			exitf("invalid -pgvector-auto-migrate value %q: %v", pgvectorAutoMigrate, parseErr)
		}
		cfg.PGVector.AutoMigrate = enabled
	}
	if pgvectorMaxOpenConns >= 0 {
		cfg.PGVector.MaxOpenConns = pgvectorMaxOpenConns
	}
	if pgvectorMaxIdleConns >= 0 {
		cfg.PGVector.MaxIdleConns = pgvectorMaxIdleConns
	}
	if strings.TrimSpace(embeddingProvider) != "" {
		cfg.Embedding.Provider = strings.ToLower(strings.TrimSpace(embeddingProvider))
	}
	if strings.TrimSpace(embeddingFallback) != "" {
		cfg.Embedding.FallbackProvider = strings.ToLower(strings.TrimSpace(embeddingFallback))
	}
	if strings.TrimSpace(embeddingOllamaURL) != "" {
		cfg.Embedding.OllamaBaseURL = strings.TrimSpace(embeddingOllamaURL)
	}
	if strings.TrimSpace(embeddingOllamaModel) != "" {
		cfg.Embedding.OllamaModel = strings.TrimSpace(embeddingOllamaModel)
	}
	if embeddingOllamaTimeoutS >= 0 {
		cfg.Embedding.OllamaTimeoutSeconds = embeddingOllamaTimeoutS
	}
	if strings.TrimSpace(embeddingModelPath) != "" {
		cfg.Embedding.ModelPath = strings.TrimSpace(embeddingModelPath)
	}
	if strings.TrimSpace(embeddingTokenizerPath) != "" {
		cfg.Embedding.TokenizerPath = strings.TrimSpace(embeddingTokenizerPath)
	}
	if strings.TrimSpace(parserEnabled) != "" {
		enabled, parseErr := strconv.ParseBool(strings.TrimSpace(parserEnabled))
		if parseErr != nil {
			exitf("invalid -parser-enabled value %q: %v", parserEnabled, parseErr)
		}
		cfg.Parser.Enabled = enabled
	}
	if strings.TrimSpace(parserProvider) != "" {
		cfg.Parser.Provider = strings.ToLower(strings.TrimSpace(parserProvider))
	}
	if strings.TrimSpace(parserOllamaModel) != "" {
		cfg.Parser.OllamaModel = strings.TrimSpace(parserOllamaModel)
	}
	if strings.TrimSpace(parserOpenRouterModel) != "" {
		cfg.Parser.OpenRouterModel = strings.TrimSpace(parserOpenRouterModel)
	}

	if err := config.Validate(cfg); err != nil {
		exitf("rendered config is invalid: %v", err)
	}

	b, err := yaml.Marshal(cfg)
	if err != nil {
		exitf("marshal yaml: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		exitf("create output dir: %v", err)
	}
	if err := os.WriteFile(outPath, b, 0o644); err != nil {
		exitf("write output: %v", err)
	}
}

func exitf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
