// Package startup wires the full runtime for local and server entrypoints.
package startup

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/pali-mem/pali/internal/config"
	corememory "github.com/pali-mem/pali/internal/core/memory"
	coretenant "github.com/pali-mem/pali/internal/core/tenant"
	"github.com/pali-mem/pali/internal/embeddings"
	palimcp "github.com/pali-mem/pali/internal/mcp"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	"github.com/pali-mem/pali/internal/wiring"
)

// MCPRuntime owns the server and its shutdown cleanup.
type MCPRuntime struct {
	Server  *palimcp.Server
	Cleanup func()
}

// NewMCPRuntime constructs a runnable MCP runtime from config.
func NewMCPRuntime(cfg config.Config) (*MCPRuntime, error) {
	db, err := sqliterepo.Open(context.Background(), cfg.Database.SQLiteDSN)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	cleanup := func() {
		if err := db.Close(); err != nil {
			log.Printf("db close error: %v", err)
		}
	}
	fail := func(err error) (*MCPRuntime, error) {
		cleanup()
		return nil, err
	}

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	entityFactRepo, entityFactCleanup, err := wiring.BuildEntityFactRepository(cfg, db)
	if err != nil {
		return fail(fmt.Errorf("build entity fact repository: %w", err))
	}

	stopPostprocess := func() {}
	cleanup = chainCleanup(cleanup, func() {
		stopPostprocess()
		if err := entityFactCleanup(); err != nil {
			log.Printf("entity fact repo close error: %v", err)
		}
	})

	vectorStore, vectorCleanup, err := wiring.BuildVectorStore(cfg, db)
	if err != nil {
		return fail(fmt.Errorf("build vector store: %w", err))
	}
	cleanup = chainCleanup(cleanup, func() {
		if err := vectorCleanup(); err != nil {
			log.Printf("vector store close error: %v", err)
		}
	})
	embedder, embedMeta, err := embeddings.BuildWithMetadata(cfg)
	if err != nil {
		return fail(fmt.Errorf("build embedder: %w", err))
	}
	scorer, err := wiring.BuildImportanceScorer(cfg)
	if err != nil {
		return fail(fmt.Errorf("build scorer: %w", err))
	}
	infoParser, err := wiring.BuildInfoParser(cfg)
	if err != nil {
		return fail(fmt.Errorf("build parser: %w", err))
	}
	decomposer, err := wiring.BuildMultiHopQueryDecomposer(cfg)
	if err != nil {
		return fail(fmt.Errorf("build multi-hop decomposer: %w", err))
	}

	serviceOptions := wiring.BuildMemoryServiceOptions(cfg, infoParser, entityFactRepo, decomposer)
	memoryService := corememory.NewService(memoryRepo, tenantRepo, vectorStore, embedder, scorer, serviceOptions...)
	if cfg.Postprocess.Enabled {
		stop, err := memoryService.StartPostprocessWorkers(context.Background(), corememory.PostprocessWorkerOptions{
			Enabled:      cfg.Postprocess.Enabled,
			PollInterval: time.Duration(cfg.Postprocess.PollIntervalMS) * time.Millisecond,
			BatchSize:    cfg.Postprocess.BatchSize,
			WorkerCount:  cfg.Postprocess.WorkerCount,
			Lease:        time.Duration(cfg.Postprocess.LeaseMS) * time.Millisecond,
			MaxAttempts:  cfg.Postprocess.MaxAttempts,
			RetryBase:    time.Duration(cfg.Postprocess.RetryBaseMS) * time.Millisecond,
			RetryMax:     time.Duration(cfg.Postprocess.RetryMaxMS) * time.Millisecond,
		})
		if err != nil {
			return fail(fmt.Errorf("start postprocess workers: %w", err))
		}
		stopPostprocess = stop
	}

	tenantService := coretenant.NewService(tenantRepo)
	server, err := palimcp.NewServer(palimcp.Services{
		Memory: memoryService,
		Tenant: tenantService,
	}, palimcp.Options{
		DefaultTenantID: cfg.DefaultTenantID,
		AuthEnabled:     cfg.Auth.Enabled,
		Logger:          log.Default(),
	})
	if err != nil {
		return fail(fmt.Errorf("build mcp server: %w", err))
	}

	Log(cfg, tenantRepo, memoryRepo, embedMeta)
	return &MCPRuntime{
		Server:  server,
		Cleanup: cleanup,
	}, nil
}

func chainCleanup(first, next func()) func() {
	return func() {
		if next != nil {
			next()
		}
		if first != nil {
			first()
		}
	}
}
