package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/pali-mem/pali/internal/api"
	"github.com/pali-mem/pali/internal/config"
	corememory "github.com/pali-mem/pali/internal/core/memory"
	coretenant "github.com/pali-mem/pali/internal/core/tenant"
	"github.com/pali-mem/pali/internal/embeddings"
	palimcp "github.com/pali-mem/pali/internal/mcp"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	"github.com/pali-mem/pali/internal/startup"
	"github.com/pali-mem/pali/internal/wiring"
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
	entityFactRepo, entityFactCleanup, err := wiring.BuildEntityFactRepository(cfg, db)
	if err != nil {
		log.Fatalf("build entity fact repository: %v", err)
	}
	defer func() {
		if err := entityFactCleanup(); err != nil {
			log.Printf("entity fact repo close error: %v", err)
		}
	}()
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
	decomposer, err := wiring.BuildMultiHopQueryDecomposer(cfg)
	if err != nil {
		log.Fatalf("build multi-hop decomposer: %v", err)
	}
	serviceOptions := wiring.BuildMemoryServiceOptions(cfg, infoParser, entityFactRepo, decomposer)
	memoryService := corememory.NewService(memoryRepo, tenantRepo, vectorStore, embedder, scorer, serviceOptions...)
	stopPostprocess := func() {}
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
			log.Fatalf("start postprocess workers: %v", err)
		}
		stopPostprocess = stop
	}
	defer stopPostprocess()
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

	startup.Log(cfg, tenantRepo, memoryRepo, embedMeta)
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
