package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExampleConfigMatchesDefaults(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("NEO4J_PASSWORD", "")

	examplePath := filepath.Clean(filepath.Join("..", "..", "pali.yaml.example"))
	cfg, err := Load(examplePath)
	require.NoError(t, err)
	require.Equal(t, Defaults(), cfg)
}

func TestProviderProfilesAreValid(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("NEO4J_PASSWORD", "")

	profiles := []string{
		"mock.yaml",
		"lexical.yaml",
		"ollama.yaml",
		"qdrant-ollama.yaml",
		"qdrant-neo4j-lexical.yaml",
	}
	for _, profile := range profiles {
		profile := profile
		t.Run(profile, func(t *testing.T) {
			if strings.Contains(profile, "neo4j") {
				t.Setenv("NEO4J_PASSWORD", "test-password")
			}
			path := filepath.Clean(filepath.Join("..", "..", "test", "config", "providers", profile))
			_, err := Load(path)
			require.NoError(t, err)
		})
	}
}
