package embeddings

import (
	"testing"

	"github.com/pali-mem/pali/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBuildWithMetadata_PrimaryProvider(t *testing.T) {
	cfg := config.Defaults()
	cfg.Embedding.Provider = "lexical"
	cfg.Embedding.FallbackProvider = ""

	embedder, meta, err := BuildWithMetadata(cfg)
	require.NoError(t, err)
	require.NotNil(t, embedder)
	require.Equal(t, "lexical", meta.PrimaryProvider)
	require.Equal(t, "lexical", meta.ResolvedProvider)
	require.False(t, meta.UsedFallback)
}

func TestBuildWithMetadata_FallbackProviderUsed(t *testing.T) {
	cfg := config.Defaults()
	cfg.Embedding.Provider = "onnx"
	cfg.Embedding.FallbackProvider = "lexical"
	cfg.Embedding.ModelPath = "./does-not-exist.onnx"
	cfg.Embedding.TokenizerPath = "./does-not-exist.json"

	embedder, meta, err := BuildWithMetadata(cfg)
	require.NoError(t, err)
	require.NotNil(t, embedder)
	require.Equal(t, "onnx", meta.PrimaryProvider)
	require.Equal(t, "lexical", meta.ResolvedProvider)
	require.Equal(t, "lexical", meta.FallbackProvider)
	require.True(t, meta.UsedFallback)
}

func TestBuildWithMetadata_FallbackDisabledFailsFast(t *testing.T) {
	cfg := config.Defaults()
	cfg.Embedding.Provider = "onnx"
	cfg.Embedding.FallbackProvider = ""
	cfg.Embedding.ModelPath = "./does-not-exist.onnx"
	cfg.Embedding.TokenizerPath = "./does-not-exist.json"

	embedder, meta, err := BuildWithMetadata(cfg)
	require.Error(t, err)
	require.Nil(t, embedder)
	require.Equal(t, "onnx", meta.PrimaryProvider)
	require.False(t, meta.UsedFallback)
}

func TestBuildWithMetadata_OpenRouterPrimaryProvider(t *testing.T) {
	cfg := config.Defaults()
	cfg.Embedding.Provider = "openrouter"
	cfg.Embedding.FallbackProvider = "lexical"
	cfg.OpenRouter.APIKey = "test-key"
	cfg.OpenRouter.EmbeddingModel = "openai/text-embedding-3-small:nitro"

	embedder, meta, err := BuildWithMetadata(cfg)
	require.NoError(t, err)
	require.NotNil(t, embedder)
	require.Equal(t, "openrouter", meta.PrimaryProvider)
	require.Equal(t, "openrouter", meta.ResolvedProvider)
	require.False(t, meta.UsedFallback)
}
