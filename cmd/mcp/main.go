package main

import (
	"context"
	"flag"
	"log"

	corememory "github.com/vein05/pali/internal/core/memory"
	coretenant "github.com/vein05/pali/internal/core/tenant"
	"github.com/vein05/pali/internal/embeddings"
	palimcp "github.com/vein05/pali/internal/mcp"
	sqliterepo "github.com/vein05/pali/internal/repository/sqlite"
	"github.com/vein05/pali/internal/wiring"

	"github.com/vein05/pali/internal/config"
)

func main() {
	cfgPath := flag.String("config", "pali.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := sqliterepo.Open(context.Background(), cfg.Database.SQLiteDSN)
	if err != nil {
		log.Fatalf("open sqlite: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("db close error: %v", err)
		}
	}()

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	entityFactRepo := sqliterepo.NewEntityFactRepository(db)
	vectorStore, err := wiring.BuildVectorStore(cfg, db)
	if err != nil {
		log.Fatalf("build vector store: %v", err)
	}
	embedder, embedMeta, err := embeddings.BuildWithMetadata(cfg)
	if err != nil {
		log.Fatalf("build embedder: %v", err)
	}
	scorer, err := wiring.BuildImportanceScorer(cfg)
	if err != nil {
		log.Fatalf("build scorer: %v", err)
	}
	infoParser, err := wiring.BuildInfoParser(cfg)
	if err != nil {
		log.Fatalf("build parser: %v", err)
	}
	serviceOptions := []corememory.ServiceOption{
		corememory.StructuredMemoryOptions{
			Enabled:               cfg.StructuredMemory.Enabled,
			DualWriteObservations: cfg.StructuredMemory.DualWriteObservations,
			DualWriteEvents:       cfg.StructuredMemory.DualWriteEvents,
			MaxObservations:       cfg.StructuredMemory.MaxObservations,
		},
		corememory.RankingOptions{
			Algorithm: cfg.Retrieval.Scoring.Algorithm,
			WAL: corememory.WALWeights{
				Recency:    cfg.Retrieval.Scoring.WAL.Recency,
				Relevance:  cfg.Retrieval.Scoring.WAL.Relevance,
				Importance: cfg.Retrieval.Scoring.WAL.Importance,
			},
			Match: corememory.MatchWeights{
				Recency:      cfg.Retrieval.Scoring.Match.Recency,
				Relevance:    cfg.Retrieval.Scoring.Match.Relevance,
				Importance:   cfg.Retrieval.Scoring.Match.Importance,
				QueryOverlap: cfg.Retrieval.Scoring.Match.QueryOverlap,
				Routing:      cfg.Retrieval.Scoring.Match.Routing,
			},
		},
		corememory.ParserOptions{
			Enabled:         cfg.Parser.Enabled,
			Provider:        cfg.Parser.Provider,
			StoreRawTurn:    cfg.Parser.StoreRawTurn,
			MaxFacts:        cfg.Parser.MaxFacts,
			DedupeThreshold: cfg.Parser.DedupeThreshold,
			UpdateThreshold: cfg.Parser.UpdateThreshold,
		},
	}
	if infoParser != nil {
		serviceOptions = append(serviceOptions, corememory.WithInfoParser(infoParser))
	}
	serviceOptions = append(serviceOptions, corememory.WithEntityFactRepository(entityFactRepo))
	memoryService := corememory.NewService(memoryRepo, tenantRepo, vectorStore, embedder, scorer, serviceOptions...)
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
		log.Fatalf("build mcp server: %v", err)
	}

	if embedMeta.UsedFallback {
		log.Printf("[pali-startup] embedder=%s (fallback from %s)", embedMeta.ResolvedProvider, embedMeta.PrimaryProvider)
	} else {
		log.Printf("[pali-startup] embedder=%s", embedMeta.ResolvedProvider)
	}
	log.Printf("starting pali mcp server over stdio")
	if err := server.RunStdio(context.Background()); err != nil {
		log.Fatalf("mcp server exited: %v", err)
	}
}
