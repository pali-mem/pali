package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/vein05/pali/internal/api"
	"github.com/vein05/pali/internal/config"
	corememory "github.com/vein05/pali/internal/core/memory"
	coretenant "github.com/vein05/pali/internal/core/tenant"
	"github.com/vein05/pali/internal/embeddings"
	palimcp "github.com/vein05/pali/internal/mcp"
	sqliterepo "github.com/vein05/pali/internal/repository/sqlite"
	"github.com/vein05/pali/internal/wiring"
)

const (
	modeAPI = "api"
	modeMCP = "mcp"
)

var errHelp = errors.New("help requested")

func main() {
	mode, cfgPath, err := parseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, errHelp) {
			usage(os.Stdout)
			return
		}
		fmt.Fprintf(os.Stderr, "ERROR: %v\n\n", err)
		usage(os.Stderr)
		os.Exit(1)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch mode {
	case modeMCP:
		runMCP(cfg)
	default:
		runAPI(cfg)
	}
}

func runAPI(cfg config.Config) {
	router, cleanup, err := api.NewRouter(cfg)
	if err != nil {
		log.Fatalf("create router: %v", err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			log.Printf("cleanup error: %v", err)
		}
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("[pali-startup] starting pali server on http://localhost:%d", cfg.Server.Port)
	if err := router.Run(addr); err != nil {
		log.Fatalf("server exited: %v", err)
	}
}

func runMCP(cfg config.Config) {
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
			Model:           cfg.Parser.OllamaModel,
			StoreRawTurn:    cfg.Parser.StoreRawTurn,
			MaxFacts:        cfg.Parser.MaxFacts,
			DedupeThreshold: cfg.Parser.DedupeThreshold,
			UpdateThreshold: cfg.Parser.UpdateThreshold,
		},
		corememory.WithLogger(log.Default()),
		corememory.WithDebug(cfg.Logging.DevVerbose, cfg.Logging.Progress),
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

	log.Printf("[pali-startup] pid=%d port=%d db=%s", os.Getpid(), cfg.Server.Port, cfg.Database.SQLiteDSN)
	if embedMeta.UsedFallback {
		log.Printf(
			"[pali-startup] embedder=%s (fallback from %s) model=%s provider=%s",
			embedMeta.ResolvedProvider,
			embedMeta.PrimaryProvider,
			cfg.Embedding.OllamaModel,
			cfg.Embedding.OllamaBaseURL,
		)
	} else {
		log.Printf(
			"[pali-startup] embedder=%s model=%s provider=%s",
			embedMeta.ResolvedProvider,
			cfg.Embedding.OllamaModel,
			cfg.Embedding.OllamaBaseURL,
		)
	}
	var tenantCount int64
	var memoryCount int64
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM tenants").Scan(&tenantCount); err == nil {
		if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM memories").Scan(&memoryCount); err == nil {
			log.Printf("[pali-startup] tenant_count=%d memory_count=%d", tenantCount, memoryCount)
		}
	}
	log.Printf("starting pali mcp server over stdio")
	if err := server.RunStdio(context.Background()); err != nil {
		log.Fatalf("mcp server exited: %v", err)
	}
}

func parseArgs(args []string) (string, string, error) {
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			return "", "", errHelp
		}
	}

	if len(args) > 0 && args[0] == "mcp" {
		return parseModeFlags(modeMCP, trimRunToken(args[1:]))
	}
	if len(args) > 0 && args[0] == "api" {
		return parseModeFlags(modeAPI, trimRunToken(args[1:]))
	}
	if len(args) > 0 && args[0] == "run" {
		return parseModeFlags(modeAPI, args[1:])
	}

	mode, cfgPath, err := parseModeFlags(modeAPI, args)
	if err != nil {
		return "", "", err
	}
	return mode, cfgPath, nil
}

func parseModeFlags(mode string, args []string) (string, string, error) {
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfgPath := fs.String("config", "pali.yaml", "Path to config file")
	if err := fs.Parse(args); err != nil {
		return "", "", err
	}
	if fs.NArg() > 0 {
		return "", "", fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return mode, *cfgPath, nil
}

func trimRunToken(args []string) []string {
	if len(args) > 0 && args[0] == "run" {
		return args[1:]
	}
	return args
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  pali [-config <path>]               # start API server (default)")
	fmt.Fprintln(w, "  pali api run [-config <path>]       # start API server")
	fmt.Fprintln(w, "  pali mcp run [-config <path>]       # start MCP server over stdio")
}
