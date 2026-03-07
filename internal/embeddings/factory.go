package embeddings

import (
	"fmt"
	"strings"
	"time"

	"github.com/vein05/pali/internal/config"
	"github.com/vein05/pali/internal/domain"
	embedmock "github.com/vein05/pali/internal/embeddings/mock"
	embedollama "github.com/vein05/pali/internal/embeddings/ollama"
	onnxembed "github.com/vein05/pali/internal/embeddings/onnx"
)

type BuildMetadata struct {
	PrimaryProvider  string
	ResolvedProvider string
	FallbackProvider string
	UsedFallback     bool
}

func Build(cfg config.Config) (domain.Embedder, error) {
	embedder, _, err := BuildWithMetadata(cfg)
	return embedder, err
}

func BuildWithMetadata(cfg config.Config) (domain.Embedder, BuildMetadata, error) {
	primary := providerOrDefault(cfg.Embedding.Provider)
	embedder, err := buildProvider(primary, cfg)
	if err == nil {
		return embedder, BuildMetadata{
			PrimaryProvider:  primary,
			ResolvedProvider: primary,
		}, nil
	}

	fallback := strings.ToLower(strings.TrimSpace(cfg.Embedding.FallbackProvider))
	if fallback == "" || fallback == primary {
		return nil, BuildMetadata{
			PrimaryProvider: primary,
		}, err
	}

	fallbackEmbedder, fallbackErr := buildProvider(fallback, cfg)
	if fallbackErr != nil {
		return nil, BuildMetadata{
			PrimaryProvider:  primary,
			FallbackProvider: fallback,
		}, fmt.Errorf("initialize embedding provider %q failed: %w; fallback %q also failed: %v", primary, err, fallback, fallbackErr)
	}
	return fallbackEmbedder, BuildMetadata{
		PrimaryProvider:  primary,
		ResolvedProvider: fallback,
		FallbackProvider: fallback,
		UsedFallback:     true,
	}, nil
}

func buildProvider(provider string, cfg config.Config) (domain.Embedder, error) {
	switch provider {
	case "onnx":
		e, err := onnxembed.NewEmbedder(cfg.Embedding.ModelPath, cfg.Embedding.TokenizerPath)
		if err != nil {
			return nil, fmt.Errorf("initialize onnx embedder: %w", err)
		}
		return e, nil
	case "ollama":
		timeout := time.Duration(cfg.Embedding.OllamaTimeoutSeconds) * time.Second
		e, err := embedollama.NewEmbedder(cfg.Embedding.OllamaBaseURL, cfg.Embedding.OllamaModel, timeout)
		if err != nil {
			return nil, fmt.Errorf("initialize ollama embedder: %w", err)
		}
		return e, nil
	case "lexical", "mock", "":
		return embedmock.NewEmbedder(), nil
	default:
		return nil, fmt.Errorf("unsupported embedding provider: %s", provider)
	}
}

func providerOrDefault(provider string) string {
	normalized := strings.ToLower(strings.TrimSpace(provider))
	if normalized == "" {
		return "lexical"
	}
	return normalized
}
