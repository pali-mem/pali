package wiring

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/pali-mem/pali/internal/config"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	heuristicscorer "github.com/pali-mem/pali/internal/scorer/heuristic"
	openrouterscorer "github.com/pali-mem/pali/internal/scorer/openrouter"
	"github.com/stretchr/testify/require"
)

func TestBuildVectorStore_SQLite(t *testing.T) {
	db, err := sqliterepo.Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	defer sqliterepo.CloseDBForTest(db)

	cfg := config.Defaults()
	cfg.VectorBackend = "sqlite"

	store, cleanup, err := BuildVectorStore(cfg, db)
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NotNil(t, cleanup)
	require.NoError(t, cleanup())
}

func TestBuildVectorStore_Qdrant(t *testing.T) {
	db, err := sqliterepo.Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	defer sqliterepo.CloseDBForTest(db)

	cfg := config.Defaults()
	cfg.VectorBackend = "qdrant"
	store, cleanup, err := BuildVectorStore(cfg, db)
	require.NoError(t, err)
	require.NotNil(t, store)
	require.NotNil(t, cleanup)
	require.NoError(t, cleanup())
}

func TestBuildVectorStore_PGVector(t *testing.T) {
	db, err := sqliterepo.Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	defer sqliterepo.CloseDBForTest(db)

	cfg := config.Defaults()
	cfg.VectorBackend = "pgvector"
	cfg.PGVector.DSN = "postgres://user:pass@localhost:5432/pali"

	store, cleanup, buildErr := BuildVectorStore(cfg, db)
	require.NoError(t, buildErr)
	require.NotNil(t, store)
	require.NotNil(t, cleanup)
	require.NoError(t, cleanup())
}

func TestBuildEntityFactRepository_SQLite(t *testing.T) {
	db, err := sqliterepo.Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	defer sqliterepo.CloseDBForTest(db)

	cfg := config.Defaults()
	cfg.EntityFactBackend = "sqlite"

	repo, cleanup, err := BuildEntityFactRepository(cfg, db)
	require.NoError(t, err)
	require.NotNil(t, repo)
	require.NotNil(t, cleanup)
	require.NoError(t, cleanup())
}

func TestBuildEntityFactRepository_Neo4j(t *testing.T) {
	db, err := sqliterepo.Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	defer sqliterepo.CloseDBForTest(db)

	cfg := config.Defaults()
	cfg.EntityFactBackend = "neo4j"
	cfg.Neo4j.Password = "secret"
	if password := strings.TrimSpace(os.Getenv("NEO4J_PASSWORD")); password != "" {
		cfg.Neo4j.Password = password
	}

	repo, cleanup, err := BuildEntityFactRepository(cfg, db)
	if err != nil {
		require.Contains(t, err.Error(), "initialize neo4j entity fact repository")
		return
	}
	require.NoError(t, err)
	require.NotNil(t, repo)
	require.NotNil(t, cleanup)
	require.NoError(t, cleanup())
}

func TestBuildEntityFactRepository_Unsupported(t *testing.T) {
	db, err := sqliterepo.Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	defer sqliterepo.CloseDBForTest(db)

	cfg := config.Defaults()
	cfg.EntityFactBackend = "unsupported"
	repo, cleanup, err := BuildEntityFactRepository(cfg, db)
	require.Nil(t, repo)
	require.Nil(t, cleanup)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported entity_fact_backend")
}

func TestBuildImportanceScorer_DefaultHeuristic(t *testing.T) {
	cfg := config.Defaults()
	cfg.ImportanceScorer = ""

	scorer, err := BuildImportanceScorer(cfg)
	require.NoError(t, err)
	require.IsType(t, &heuristicscorer.Scorer{}, scorer)
}

func TestBuildImportanceScorer_Unsupported(t *testing.T) {
	cfg := config.Defaults()
	cfg.ImportanceScorer = "unsupported"

	scorer, err := BuildImportanceScorer(cfg)
	require.Nil(t, scorer)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported importance_scorer")
}

func TestBuildImportanceScorer_OpenRouter(t *testing.T) {
	cfg := config.Defaults()
	cfg.ImportanceScorer = "openrouter"
	cfg.OpenRouter.APIKey = "test-key"
	cfg.OpenRouter.ScoringModel = "openai/gpt-oss-120b:nitro"

	scorer, err := BuildImportanceScorer(cfg)
	require.NoError(t, err)
	require.IsType(t, &openrouterscorer.Scorer{}, scorer)
}
