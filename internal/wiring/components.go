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
	heuristicscorer "github.com/pali-mem/pali/internal/scorer/heuristic"
	ollamascorer "github.com/pali-mem/pali/internal/scorer/ollama"
	qdrantstore "github.com/pali-mem/pali/internal/vectorstore/qdrant"
	sqlitevec "github.com/pali-mem/pali/internal/vectorstore/sqlitevec"
)

func BuildVectorStore(cfg config.Config, db *sql.DB) (domain.VectorStore, error) {
	backend := strings.ToLower(strings.TrimSpace(cfg.VectorBackend))
	if backend == "" {
		backend = "sqlite"
	}

	switch backend {
	case "sqlite":
		return sqlitevec.NewStore(db), nil
	case "qdrant":
		timeout := time.Duration(cfg.Qdrant.TimeoutMS) * time.Millisecond
		client, err := qdrantstore.NewClient(cfg.Qdrant.BaseURL, cfg.Qdrant.APIKey, cfg.Qdrant.Collection, timeout)
		if err != nil {
			return nil, fmt.Errorf("initialize qdrant client: %w", err)
		}
		return qdrantstore.NewStore(client), nil
	case "pgvector":
		return nil, fmt.Errorf("vector_backend=pgvector is not implemented yet; use sqlite for now")
	default:
		return nil, fmt.Errorf("unsupported vector_backend: %q", cfg.VectorBackend)
	}
}

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
	default:
		return nil, fmt.Errorf("unsupported importance_scorer: %q", cfg.ImportanceScorer)
	}
}

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
	default:
		return nil, fmt.Errorf("unsupported parser provider: %q", cfg.Parser.Provider)
	}
}
