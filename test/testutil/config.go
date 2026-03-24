package testutil

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/pali-mem/pali/internal/config"
	"github.com/stretchr/testify/require"
)

// MustLoadProviderConfig loads a provider config from the test fixtures.
func MustLoadProviderConfig(t *testing.T, profile string) config.Config {
	t.Helper()
	cfg, err := config.Load(providerConfigPath(profile))
	require.NoError(t, err)
	return cfg
}

func providerConfigPath(profile string) string {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	return filepath.Join(repoRoot, "test", "config", "providers", profile+".yaml")
}
