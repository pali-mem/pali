package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_PALIOverridesTakePrecedence(t *testing.T) {
	t.Setenv("PALI_SERVER_HOST", "0.0.0.0")
	t.Setenv("PALI_SERVER_PORT", "19090")
	t.Setenv("PALI_VECTOR_BACKEND", "qdrant")
	t.Setenv("PALI_QDRANT_BASE_URL", "http://qdrant:6333")
	t.Setenv("PALI_QDRANT_COLLECTION", "pali_docker")
	t.Setenv("PALI_PGVECTOR_TABLE", "ignored_for_qdrant")

	dir := t.TempDir()
	path := filepath.Join(dir, "pali.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
server:
  host: 127.0.0.1
  port: 8080
vector_backend: sqlite
qdrant:
  base_url: http://127.0.0.1:6333
  collection: pali_memories
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0", cfg.Server.Host)
	require.Equal(t, 19090, cfg.Server.Port)
	require.Equal(t, "qdrant", cfg.VectorBackend)
	require.Equal(t, "http://qdrant:6333", cfg.Qdrant.BaseURL)
	require.Equal(t, "pali_docker", cfg.Qdrant.Collection)
	require.Equal(t, "ignored_for_qdrant", cfg.PGVector.Table)
}

func TestLoad_PALISecretOverridesTakePrecedence(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "legacy-key")
	t.Setenv("NEO4J_PASSWORD", "legacy-pass")
	t.Setenv("PALI_OPENROUTER_API_KEY", "pali-key")
	t.Setenv("PALI_NEO4J_PASSWORD", "pali-pass")

	dir := t.TempDir()
	path := filepath.Join(dir, "pali.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
openrouter:
  api_key: yaml-key
neo4j:
  password: yaml-pass
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "pali-key", cfg.OpenRouter.APIKey)
	require.Equal(t, "pali-pass", cfg.Neo4j.Password)
}

func TestLoad_InvalidPALIOverrideReturnsError(t *testing.T) {
	t.Setenv("PALI_SERVER_PORT", "not-a-number")

	_, err := Load("")
	require.Error(t, err)
	require.Contains(t, err.Error(), `PALI_SERVER_PORT="not-a-number"`)
}

func TestLoad_PGVectorOverridesTakePrecedence(t *testing.T) {
	t.Setenv("PALI_VECTOR_BACKEND", "pgvector")
	t.Setenv("PALI_PGVECTOR_DSN", "postgres://env-user:env-pass@localhost:5432/envdb")
	t.Setenv("PALI_PGVECTOR_TABLE", "env_vectors")
	t.Setenv("PALI_PGVECTOR_AUTO_MIGRATE", "false")
	t.Setenv("PALI_PGVECTOR_MAX_OPEN_CONNS", "20")
	t.Setenv("PALI_PGVECTOR_MAX_IDLE_CONNS", "8")

	dir := t.TempDir()
	path := filepath.Join(dir, "pali.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
vector_backend: sqlite
pgvector:
  dsn: postgres://yaml-user:yaml-pass@localhost:5432/yamldb
  table: yaml_vectors
  auto_migrate: true
  max_open_conns: 3
  max_idle_conns: 2
`), 0o644))

	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "pgvector", cfg.VectorBackend)
	require.Equal(t, "postgres://env-user:env-pass@localhost:5432/envdb", cfg.PGVector.DSN)
	require.Equal(t, "env_vectors", cfg.PGVector.Table)
	require.False(t, cfg.PGVector.AutoMigrate)
	require.Equal(t, 20, cfg.PGVector.MaxOpenConns)
	require.Equal(t, 8, cfg.PGVector.MaxIdleConns)
}
