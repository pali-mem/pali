package config

func Defaults() Config {
	return Config{
		Server: ServerConfig{
			Host: "127.0.0.1",
			Port: 8080,
		},
		VectorBackend:    "sqlite",
		DefaultTenantID:  "",
		ImportanceScorer: "heuristic",
		Postprocess: PostprocessConfig{
			Enabled:        true,
			PollIntervalMS: 250,
			BatchSize:      32,
			WorkerCount:    2,
			LeaseMS:        30000,
			MaxAttempts:    5,
			RetryBaseMS:    500,
			RetryMaxMS:     60000,
		},
		StructuredMemory: StructuredMemoryConfig{
			Enabled:               false,
			DualWriteObservations: false,
			DualWriteEvents:       false,
			MaxObservations:       3,
		},
		Retrieval: RetrievalConfig{
			Scoring: RetrievalScoringConfig{
				Algorithm: "wal",
				WAL: ScoringWeightsConfig{
					Recency:    0.1,
					Relevance:  0.8,
					Importance: 0.1,
				},
				Match: MatchScoringWeightsConfig{
					Recency:      0.05,
					Relevance:    0.70,
					Importance:   0.10,
					QueryOverlap: 0.10,
					Routing:      0.05,
				},
			},
		},
		Parser: ParserConfig{
			Enabled:         false,
			Provider:        "heuristic",
			OllamaBaseURL:   "http://127.0.0.1:11434",
			OllamaModel:     "deepseek-r1:7b",
			OllamaTimeoutMS: 20000,
			StoreRawTurn:    true,
			MaxFacts:        4,
			DedupeThreshold: 0.88,
			UpdateThreshold: 0.94,
		},
		Database: Database{
			SQLiteDSN: "file:pali.db?cache=shared",
		},
		Qdrant: QdrantConfig{
			BaseURL:    "http://127.0.0.1:6333",
			APIKey:     "",
			Collection: "pali_memories",
			TimeoutMS:  2000,
		},
		Embedding: Embedding{
			Provider:             "ollama",
			FallbackProvider:     "",
			ModelPath:            "./models/all-MiniLM-L6-v2/model.onnx",
			TokenizerPath:        "./models/all-MiniLM-L6-v2/tokenizer.json",
			OllamaBaseURL:        "http://127.0.0.1:11434",
			OllamaModel:          "mxbai-embed-large",
			OllamaTimeoutSeconds: 10,
		},
		Ollama: OllamaConfig{
			BaseURL:   "http://127.0.0.1:11434",
			Model:     "deepseek-r1:7b",
			TimeoutMS: 2000,
		},
		Auth: AuthConfig{
			Enabled:   false,
			JWTSecret: "",
			Issuer:    "pali",
		},
		Logging: LoggingConfig{
			DevVerbose: false,
			Progress:   true,
		},
	}
}
