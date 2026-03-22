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
	"github.com/pali-mem/pali/internal/bootstrap"
	"github.com/pali-mem/pali/internal/config"
	"github.com/pali-mem/pali/internal/startup"
)

const (
	commandAPI  = "api"
	commandMCP  = "mcp"
	commandInit = "init"
)

var errHelp = errors.New("help requested")

type cliCommand struct {
	name    string
	cfgPath string
	init    bootstrap.Options
}

func main() {
	cmd, err := parseArgs(os.Args[1:])
	if err != nil {
		if errors.Is(err, errHelp) {
			usage(os.Stdout)
			return
		}
		fmt.Fprintf(os.Stderr, "ERROR: %v\n\n", err)
		usage(os.Stderr)
		os.Exit(1)
	}

	if cmd.name == commandInit {
		if err := bootstrap.Run(cmd.init, os.Stdout, os.Stderr); err != nil {
			log.Fatalf("initialize pali: %v", err)
		}
		return
	}

	cfg, err := config.Load(cmd.cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch cmd.name {
	case commandMCP:
		runMCP(cfg)
	default:
		runAPI(cfg, cmd.cfgPath)
	}
}

func runAPI(cfg config.Config, cfgPath string) {
	router, cleanup, err := api.NewRouterWithConfigPath(cfg, cfgPath)
	if err != nil {
		log.Fatalf("create router: %v", err)
	}
	defer func() {
		if err := cleanup(); err != nil {
			log.Printf("cleanup error: %v", err)
		}
	}()

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	displayAddr := displayServerAddr(cfg.Server.Host, cfg.Server.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.ListenAndServe()
	}()

	log.Printf("[pali-startup] starting pali server on %s", displayAddr)

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

func displayServerAddr(host string, port int) string {
	switch host {
	case "", "0.0.0.0", "127.0.0.1", "::", "::1":
		host = "localhost"
	}
	return fmt.Sprintf("%s:%d", host, port)
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

func parseArgs(args []string) (cliCommand, error) {
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			return cliCommand{}, errHelp
		}
	}

	if len(args) > 0 && (args[0] == "init" || args[0] == "setup") {
		return parseInitFlags(args[1:])
	}

	if len(args) > 0 && args[0] == "mcp" {
		return parseModeFlags(commandMCP, trimServeToken(args[1:]))
	}
	if len(args) > 0 && (args[0] == "api" || args[0] == "serve") {
		return parseModeFlags(commandAPI, trimServeToken(args[1:]))
	}
	if len(args) > 0 && args[0] == "run" {
		return parseModeFlags(commandAPI, args[1:])
	}

	return parseModeFlags(commandAPI, args)
}

func parseModeFlags(name string, args []string) (cliCommand, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfgPath := fs.String("config", "pali.yaml", "Path to config file")
	if err := fs.Parse(args); err != nil {
		return cliCommand{}, err
	}
	if fs.NArg() > 0 {
		return cliCommand{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return cliCommand{name: name, cfgPath: *cfgPath}, nil
}

func parseInitFlags(args []string) (cliCommand, error) {
	opts := bootstrap.DefaultOptions()
	fs := flag.NewFlagSet(commandInit, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bootstrap.AddFlags(fs, &opts)
	if err := fs.Parse(args); err != nil {
		return cliCommand{}, err
	}
	if fs.NArg() > 0 {
		return cliCommand{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return cliCommand{name: commandInit, init: opts}, nil
}

func trimServeToken(args []string) []string {
	if len(args) > 0 && (args[0] == "run" || args[0] == "serve") {
		return args[1:]
	}
	return args
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  pali init [flags]                   # create config and run setup checks")
	fmt.Fprintln(w, "  pali serve [-config <path>]         # start API server")
	fmt.Fprintln(w, "  pali [-config <path>]               # start API server (default)")
	fmt.Fprintln(w, "  pali api serve [-config <path>]     # start API server")
	fmt.Fprintln(w, "  pali mcp serve [-config <path>]     # start MCP server over stdio")
	fmt.Fprintln(w, "  pali mcp run [-config <path>]       # alias for mcp serve")
}
