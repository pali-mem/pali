package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/pali-mem/pali/internal/bootstrap"
	"github.com/stretchr/testify/require"
)

func TestParseFlagsSupportsConfig(t *testing.T) {
	opts := bootstrap.DefaultOptions()
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	bootstrap.AddFlags(fs, &opts)
	require.NoError(t, fs.Parse([]string{"-config", "configs/dev.yaml", "-skip-ollama-check"}))
	require.Equal(t, "configs/dev.yaml", opts.ConfigPath)
	require.True(t, opts.SkipOllamaCheck)
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
	require.NoError(t, bootstrap.EnsureConfig(target))

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	require.Equal(t, string(example), string(got))
}
