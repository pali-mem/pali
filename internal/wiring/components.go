// Package wiring assembles concrete implementations from configuration.
package wiring

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/pali-mem/pali/internal/config"
	corememory "github.com/pali-mem/pali/internal/core/memory"
	"github.com/pali-mem/pali/internal/domain"
	neo4jrepo "github.com/pali-mem/pali/internal/repository/neo4j"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	heuristicscorer "github.com/pali-mem/pali/internal/scorer/heuristic"
	ollamascorer "github.com/pali-mem/pali/internal/scorer/ollama"
	openrouterscorer "github.com/pali-mem/pali/internal/scorer/openrouter"
	pgvectorstore "github.com/pali-mem/pali/internal/vectorstore/pgvector"
	qdrantstore "github.com/pali-mem/pali/internal/vectorstore/qdrant"
	sqlitevec "github.com/pali-mem/pali/internal/vectorstore/sqlitevec"
)

// BuildVectorStore constructs the configured vector-store implementation.
func BuildVectorStore(cfg config.Config, db *sql.DB) (domain.VectorStore, func() error, error) {
	backend := strings.ToLower(strings.TrimSpace(cfg.VectorBackend))
	if backend == "" {
		backend = "sqlite"
	}

	switch backend {
	case "sqlite":
		return sqlitevec.NewStore(db), func() error { return nil }, nil
	case "qdrant":
		timeout := time.Duration(cfg.Qdrant.TimeoutMS) * time.Millisecond
		client, err := qdrantstore.NewClient(cfg.Qdrant.BaseURL, cfg.Qdrant.APIKey, cfg.Qdrant.Collection, timeout)
		if err != nil {
			return nil, nil, fmt.Errorf("initialize qdrant client: %w", err)
		}
		return qdrantstore.NewStore(client), func() error { return nil }, nil
	case "pgvector":
		store, err := pgvectorstore.NewStore(pgvectorstore.Options{
			DSN:          cfg.PGVector.DSN,
			Table:        cfg.PGVector.Table,
			AutoMigrate:  cfg.PGVector.AutoMigrate,
			MaxOpenConns: cfg.PGVector.MaxOpenConns,
			MaxIdleConns: cfg.PGVector.MaxIdleConns,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("initialize pgvector store: %w", err)
		}
		return store, store.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported vector_backend: %q", cfg.VectorBackend)
	}
}

// BuildEntityFactRepository constructs the configured entity-fact repository.
func BuildEntityFactRepository(cfg config.Config, db *sql.DB) (domain.EntityFactRepository, func() error, error) {
	backend := strings.ToLower(strings.TrimSpace(cfg.EntityFactBackend))
	if backend == "" {
		backend = "sqlite"
	}

	switch backend {
	case "sqlite":
		return sqliterepo.NewEntityFactRepository(db), func() error { return nil }, nil
	case "neo4j":
		timeout := time.Duration(cfg.Neo4j.TimeoutMS) * time.Millisecond
		repo, err := neo4jrepo.NewEntityFactRepository(neo4jrepo.Options{
			URI:       cfg.Neo4j.URI,
			Username:  cfg.Neo4j.Username,
			Password:  cfg.Neo4j.Password,
			Database:  cfg.Neo4j.Database,
			Timeout:   timeout,
			BatchSize: cfg.Neo4j.BatchSize,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("initialize neo4j entity fact repository: %w", err)
		}
		return repo, repo.Close, nil
	default:
		return nil, nil, fmt.Errorf("unsupported entity_fact_backend: %q", cfg.EntityFactBackend)
	}
}

// BuildImportanceScorer constructs the configured importance scorer.
func BuildImportanceScorer(cfg config.Config) (domain.ImportanceScorer, error) {
	scorer := strings.ToLower(strings.TrimSpace(cfg.ImportanceScorer))
	if scorer == "" {
		scorer = "heuristic"
	}

	switch scorer {
	case "heuristic":
		return heuristicscorer.NewScorer(), nil
	case "ollama":
		timeout := time.Duration(cfg.Ollama.TimeoutMS) * time.Millisecond
		client, err := ollamascorer.NewClient(cfg.Ollama.BaseURL, cfg.Ollama.Model, timeout)
		if err != nil {
			return nil, fmt.Errorf("initialize ollama scorer client: %w", err)
		}
		return ollamascorer.NewScorer(client), nil
	case "openrouter":
		timeout := time.Duration(cfg.OpenRouter.TimeoutMS) * time.Millisecond
		client, err := openrouterscorer.NewClient(
			cfg.OpenRouter.BaseURL,
			cfg.OpenRouter.APIKey,
			cfg.OpenRouter.ScoringModel,
			timeout,
		)
		if err != nil {
			return nil, fmt.Errorf("initialize openrouter scorer client: %w", err)
		}
		return openrouterscorer.NewScorer(client), nil
	default:
		return nil, fmt.Errorf("unsupported importance_scorer: %q", cfg.ImportanceScorer)
	}
}

// BuildInfoParser constructs the configured structured-memory parser.
func BuildInfoParser(cfg config.Config) (corememory.InfoParser, error) {
	if !cfg.Parser.Enabled {
		return nil, nil
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Parser.Provider))
	if provider == "" {
		provider = "heuristic"
	}
	switch provider {
	case "heuristic":
		return corememory.NewHeuristicInfoParser(), nil
	case "ollama":
		timeout := time.Duration(cfg.Parser.OllamaTimeoutMS) * time.Millisecond
		parser, err := corememory.NewOllamaInfoParser(
			cfg.Parser.OllamaBaseURL,
			cfg.Parser.OllamaModel,
			timeout,
			log.Default(),
			cfg.Logging.DevVerbose,
		)
		if err != nil {
			return nil, fmt.Errorf("initialize parser ollama client: %w", err)
		}
		return parser, nil
	case "openrouter":
		timeout := time.Duration(cfg.OpenRouter.TimeoutMS) * time.Millisecond
		model := strings.TrimSpace(cfg.Parser.OpenRouterModel)
		if model == "" {
			model = strings.TrimSpace(cfg.OpenRouter.ScoringModel)
		}
		client, err := openrouterscorer.NewClient(
			cfg.OpenRouter.BaseURL,
			cfg.OpenRouter.APIKey,
			model,
			timeout,
		)
		if err != nil {
			return nil, fmt.Errorf("initialize parser openrouter client: %w", err)
		}
		return corememory.NewOpenRouterInfoParser(client, log.Default(), cfg.Logging.DevVerbose), nil
	default:
		return nil, fmt.Errorf("unsupported parser provider: %q", cfg.Parser.Provider)
	}
}

// BuildMultiHopQueryDecomposer constructs the configured query decomposer.
func BuildMultiHopQueryDecomposer(cfg config.Config) (corememory.MultiHopQueryDecomposer, error) {
	if !cfg.Retrieval.MultiHop.LLMDecompositionEnabled {
		return nil, nil
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Retrieval.MultiHop.DecompositionProvider))
	if provider == "" {
		provider = "openrouter"
	}
	if provider == "none" || provider == "disabled" {
		return nil, nil
	}

	switch provider {
	case "openrouter":
		timeout := time.Duration(cfg.OpenRouter.TimeoutMS) * time.Millisecond
		model := strings.TrimSpace(cfg.Retrieval.MultiHop.OpenRouterModel)
		if model == "" {
			model = strings.TrimSpace(cfg.OpenRouter.ScoringModel)
		}
		client, err := openrouterscorer.NewClient(
			cfg.OpenRouter.BaseURL,
			cfg.OpenRouter.APIKey,
			model,
			timeout,
		)
		if err != nil {
			return nil, fmt.Errorf("initialize multi-hop openrouter decomposer client: %w", err)
		}
		return corememory.NewLLMMultiHopQueryDecomposer(client, log.Default(), cfg.Logging.DevVerbose), nil
	case "ollama":
		timeoutMS := cfg.Retrieval.MultiHop.OllamaTimeoutMS
		if timeoutMS <= 0 {
			timeoutMS = cfg.Ollama.TimeoutMS
		}
		timeout := time.Duration(timeoutMS) * time.Millisecond
		baseURL := strings.TrimSpace(cfg.Retrieval.MultiHop.OllamaBaseURL)
		if baseURL == "" {
			baseURL = strings.TrimSpace(cfg.Ollama.BaseURL)
		}
		model := strings.TrimSpace(cfg.Retrieval.MultiHop.OllamaModel)
		if model == "" {
			model = strings.TrimSpace(cfg.Ollama.Model)
		}
		client, err := ollamascorer.NewClient(baseURL, model, timeout)
		if err != nil {
			return nil, fmt.Errorf("initialize multi-hop ollama decomposer client: %w", err)
		}
		return corememory.NewLLMMultiHopQueryDecomposer(client, log.Default(), cfg.Logging.DevVerbose), nil
	default:
		return nil, fmt.Errorf("unsupported retrieval.multi_hop.decomposition_provider: %q", cfg.Retrieval.MultiHop.DecompositionProvider)
	}
}

// BuildMemoryServiceOptions returns common memory service options derived from config.
func BuildMemoryServiceOptions(
	cfg config.Config,
	infoParser corememory.InfoParser,
	entityFactRepo domain.EntityFactRepository,
	decomposer corememory.MultiHopQueryDecomposer,
) []corememory.ServiceOption {
	options := []corememory.ServiceOption{
		corememory.StructuredMemoryOptions{
			Enabled:               cfg.StructuredMemory.Enabled,
			DualWriteObservations: cfg.StructuredMemory.DualWriteObservations,
			DualWriteEvents:       cfg.StructuredMemory.DualWriteEvents,
			MaxObservations:       cfg.StructuredMemory.MaxObservations,
		},
		corememory.RankingOptions{
			Algorithm: cfg.Retrieval.Scoring.Algorithm,
			WAL: corememory.WALWeights{
				Recency:    cfg.Retrieval.Scoring.WAL.Recency,
				Relevance:  cfg.Retrieval.Scoring.WAL.Relevance,
				Importance: cfg.Retrieval.Scoring.WAL.Importance,
			},
			Match: corememory.MatchWeights{
				Recency:      cfg.Retrieval.Scoring.Match.Recency,
				Relevance:    cfg.Retrieval.Scoring.Match.Relevance,
				Importance:   cfg.Retrieval.Scoring.Match.Importance,
				QueryOverlap: cfg.Retrieval.Scoring.Match.QueryOverlap,
				Routing:      cfg.Retrieval.Scoring.Match.Routing,
			},
		},
		corememory.RetrievalBehaviorOptions{
			AnswerTypeRoutingEnabled:             cfg.Retrieval.AnswerTypeRoutingEnabled,
			EarlyRankRerankEnabled:               cfg.Retrieval.EarlyRankRerankEnabled,
			TemporalResolverEnabled:              cfg.Retrieval.TemporalResolverEnabled,
			OpenDomainAlternativeResolverEnabled: cfg.Retrieval.OpenDomainAlternativeResolverEnabled,
			ProfileSupportLinksEnabled:           cfg.ProfileLayer.SupportLinksEnabled,
			SearchTuning: corememory.RetrievalSearchTuningOptions{
				AdaptiveQueryExpansionEnabled:        cfg.Retrieval.Search.AdaptiveQueryExpansionEnabled,
				AdaptiveQueryMaxExtraQueries:         cfg.Retrieval.Search.AdaptiveQueryMaxExtraQueries,
				AdaptiveQueryWeakLexicalThreshold:    cfg.Retrieval.Search.AdaptiveQueryWeakLexicalThreshold,
				AdaptiveQueryPlanConfidenceThreshold: cfg.Retrieval.Search.AdaptiveQueryPlanConfidenceThreshold,
				CandidateWindowMultiplier:            cfg.Retrieval.Search.CandidateWindowMultiplier,
				CandidateWindowMin:                   cfg.Retrieval.Search.CandidateWindowMin,
				CandidateWindowMax:                   cfg.Retrieval.Search.CandidateWindowMax,
				CandidateWindowTemporalBoost:         cfg.Retrieval.Search.CandidateWindowTemporalBoost,
				CandidateWindowMultiHopBoost:         cfg.Retrieval.Search.CandidateWindowMultiHopBoost,
				CandidateWindowFilterBoost:           cfg.Retrieval.Search.CandidateWindowFilterBoost,
				EarlyRerankBaseWindow:                cfg.Retrieval.Search.EarlyRerankBaseWindow,
				EarlyRerankMaxWindow:                 cfg.Retrieval.Search.EarlyRerankMaxWindow,
			},
		},
		corememory.ParserOptions{
			Enabled:                    cfg.Parser.Enabled,
			Provider:                   cfg.Parser.Provider,
			Model:                      parserModel(cfg),
			StoreRawTurn:               cfg.Parser.StoreRawTurn,
			MaxFacts:                   cfg.Parser.MaxFacts,
			DedupeThreshold:            cfg.Parser.DedupeThreshold,
			UpdateThreshold:            cfg.Parser.UpdateThreshold,
			AnswerSpanRetentionEnabled: cfg.Parser.AnswerSpanRetentionEnabled,
		},
		corememory.MultiHopOptions{
			EntityFactBridgeEnabled:    cfg.Retrieval.MultiHop.EntityFactBridgeEnabled,
			LLMDecompositionEnabled:    cfg.Retrieval.MultiHop.LLMDecompositionEnabled,
			MaxDecompositionQueries:    cfg.Retrieval.MultiHop.MaxDecompositionQueries,
			EnablePairwiseRerank:       cfg.Retrieval.MultiHop.EnablePairwiseRerank,
			TokenExpansionFallback:     cfg.Retrieval.MultiHop.TokenExpansionFallback,
			GraphPathEnabled:           cfg.Retrieval.MultiHop.GraphPathEnabled,
			GraphMaxHops:               cfg.Retrieval.MultiHop.GraphMaxHops,
			GraphSeedLimit:             cfg.Retrieval.MultiHop.GraphSeedLimit,
			GraphPathLimit:             cfg.Retrieval.MultiHop.GraphPathLimit,
			GraphMinScore:              cfg.Retrieval.MultiHop.GraphMinScore,
			GraphWeight:                cfg.Retrieval.MultiHop.GraphWeight,
			GraphTemporalValidity:      cfg.Retrieval.MultiHop.GraphTemporalValidity,
			GraphSingletonInvalidation: cfg.Retrieval.MultiHop.GraphSingletonInvalidation,
		},
		corememory.WithImplicitCanonicalKindsForEntityFacts(
			strings.EqualFold(strings.TrimSpace(cfg.EntityFactBackend), "neo4j"),
		),
		corememory.WithLogger(log.Default()),
		corememory.WithDebug(cfg.Logging.DevVerbose, cfg.Logging.Progress),
	}
	if infoParser != nil {
		options = append(options, corememory.WithInfoParser(infoParser))
	}
	if decomposer != nil {
		options = append(options, corememory.WithMultiHopQueryDecomposer(decomposer))
	}
	options = append(options, corememory.WithEntityFactRepository(entityFactRepo))
	return options
}

func parserModel(cfg config.Config) string {
	provider := strings.ToLower(strings.TrimSpace(cfg.Parser.Provider))
	switch provider {
	case "openrouter":
		if strings.TrimSpace(cfg.Parser.OpenRouterModel) != "" {
			return cfg.Parser.OpenRouterModel
		}
		return cfg.OpenRouter.ScoringModel
	default:
		return cfg.Parser.OllamaModel
	}
}
