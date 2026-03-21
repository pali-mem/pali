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
