package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pali-mem/pali/internal/api"
	"github.com/pali-mem/pali/internal/config"
	"github.com/pali-mem/pali/internal/startup"
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
	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	log.Printf("[pali-startup] starting pali server on %s", addr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server exited: %v", err)
		}
	case <-ctx.Done():
		log.Printf("[pali-shutdown] shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("[pali-shutdown] graceful shutdown failed: %v", err)
			if closeErr := server.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
				log.Printf("[pali-shutdown] forced close failed: %v", closeErr)
			}
		}
		if err := <-serverErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server exited: %v", err)
		}
	}
}

func runMCP(cfg config.Config) {
	runtime, err := startup.NewMCPRuntime(cfg)
	if err != nil {
		log.Fatalf("build mcp runtime: %v", err)
	}
	defer runtime.Cleanup()

	log.Printf("starting pali mcp server over stdio")
	if err := runtime.Server.RunStdio(context.Background()); err != nil {
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
