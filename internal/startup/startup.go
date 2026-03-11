package startup

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/pali-mem/pali/internal/config"
	"github.com/pali-mem/pali/internal/domain"
	"github.com/pali-mem/pali/internal/embeddings"
)

// Log prints repository-backed startup diagnostics.
func Log(cfg config.Config, tenantRepo domain.TenantRepository, memoryRepo domain.MemoryRepository, embedMeta embeddings.BuildMetadata) {
	logger := log.Default()
	logger.Printf("[pali-startup] pid=%d port=%d db=%s", os.Getpid(), cfg.Server.Port, cfg.Database.SQLiteDSN)
	modelValue, providerValue := embeddingStartupDetails(cfg, embedMeta.ResolvedProvider)
	if embedMeta.UsedFallback {
		logger.Printf(
			"[pali-startup] embedder=%s (fallback from %s) model=%s provider=%s",
			embedMeta.ResolvedProvider,
			embedMeta.PrimaryProvider,
			modelValue,
			providerValue,
		)
	} else {
		logger.Printf(
			"[pali-startup] embedder=%s model=%s provider=%s",
			embedMeta.ResolvedProvider,
			modelValue,
			providerValue,
		)
	}

	tenantCount, memoryCount, err := Counts(context.Background(), tenantRepo, memoryRepo)
	if err != nil {
		logger.Printf("[pali-startup] startup_count_error=%v", err)
		return
	}
	logger.Printf("[pali-startup] tenant_count=%d memory_count=%d", tenantCount, memoryCount)
}

// Counts returns aggregate tenant and memory totals for startup diagnostics.
func Counts(
	ctx context.Context,
	tenantRepo domain.TenantRepository,
	memoryRepo domain.MemoryRepository,
) (int64, int64, error) {
	tenantCounter, ok := tenantRepo.(domain.TenantCountRepository)
	if !ok {
		return 0, 0, fmt.Errorf("tenant repository does not expose Count(ctx)")
	}
	memoryCounter, ok := memoryRepo.(domain.MemoryCountRepository)
	if !ok {
		return 0, 0, fmt.Errorf("memory repository does not expose Count(ctx)")
	}

	tenantCount, err := tenantCounter.Count(ctx)
	if err != nil {
		return 0, 0, err
	}
	memoryCount, err := memoryCounter.Count(ctx)
	if err != nil {
		return tenantCount, 0, err
	}
	return tenantCount, memoryCount, nil
}

func embeddingStartupDetails(cfg config.Config, provider string) (string, string) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "onnx":
		return cfg.Embedding.ModelPath, cfg.Embedding.TokenizerPath
	case "openrouter":
		return cfg.OpenRouter.EmbeddingModel, cfg.OpenRouter.BaseURL
	case "lexical", "mock", "":
		return "lexical", "local"
	default:
		return cfg.Embedding.OllamaModel, cfg.Embedding.OllamaBaseURL
	}
}
