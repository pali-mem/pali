package wiring

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/config"
	sqliterepo "github.com/vein05/pali/internal/repository/sqlite"
	heuristicscorer "github.com/vein05/pali/internal/scorer/heuristic"
)

func TestBuildVectorStore_SQLite(t *testing.T) {
	db, err := sqliterepo.Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	cfg := config.Defaults()
	cfg.VectorBackend = "sqlite"

	store, err := BuildVectorStore(cfg, db)
	require.NoError(t, err)
	require.NotNil(t, store)
}

func TestBuildVectorStore_Qdrant(t *testing.T) {
	db, err := sqliterepo.Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	cfg := config.Defaults()
	cfg.VectorBackend = "qdrant"
	store, err := BuildVectorStore(cfg, db)
	require.NoError(t, err)
	require.NotNil(t, store)
}

func TestBuildVectorStore_NotImplementedPgvector(t *testing.T) {
	db, err := sqliterepo.Open(context.Background(), "file::memory:?cache=shared")
	require.NoError(t, err)
	defer db.Close()

	cfg := config.Defaults()
	cfg.VectorBackend = "pgvector"
	store, buildErr := BuildVectorStore(cfg, db)
	require.Nil(t, store)
	require.Error(t, buildErr)
	require.Contains(t, buildErr.Error(), "not implemented")
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
