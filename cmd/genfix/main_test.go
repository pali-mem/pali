package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanContent(t *testing.T) {
	t.Parallel()

	got := cleanContent("  \"User prefers concise updates.\nPlease keep it short.\"   ")
	require.Equal(t, "User prefers concise updates. Please keep it short.", got)
}

func TestPickCategoryDeterministic(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewSource(42))
	totalWeight := 0
	for _, c := range categories {
		totalWeight += c.weight
	}

	first := pickCategory(rng, totalWeight)
	require.NotEmpty(t, first.name)
	require.NotEmpty(t, first.tags)
	require.NotEmpty(t, first.tier)
}

func TestFixtureWriter(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	w, err := newFixtureWriter(path)
	require.NoError(t, err)
	require.NoError(t, w.Write(Fixture{
		TenantID: "bench_tenant_001",
		Content:  "A",
		Tags:     []string{"preferences"},
		Tier:     "semantic",
	}))
	require.NoError(t, w.Write(Fixture{
		TenantID: "bench_tenant_002",
		Content:  "B",
		Tags:     []string{"event"},
		Tier:     "episodic",
	}))
	require.NoError(t, w.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []Fixture
	require.NoError(t, json.Unmarshal(data, &out))
	require.Len(t, out, 2)
	require.Equal(t, "A", out[0].Content)
	require.Equal(t, "B", out[1].Content)
}
