package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseFlagsSupportsConfig(t *testing.T) {
	opts := parseFlags([]string{"-config", "configs/dev.yaml", "-skip-ollama-check"})
	require.Equal(t, "configs/dev.yaml", opts.configPath)
	require.True(t, opts.skipOllamaCheck)
}

func TestEnsureConfigCopiesExampleToCustomPath(t *testing.T) {
	tmpDir := t.TempDir()
	prevWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() {
		require.NoError(t, os.Chdir(prevWD))
	})

	example := []byte("server:\n  host: 127.0.0.1\n")
	require.NoError(t, os.WriteFile("pali.yaml.example", example, 0o644))

	target := filepath.Join("configs", "release.yaml")
	require.NoError(t, ensureConfig(target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, string(example), string(got))
}
