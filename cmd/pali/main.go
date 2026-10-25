// Command pali runs the main API or MCP server entrypoint.
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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pali-mem/pali/internal/api"
	"github.com/pali-mem/pali/internal/bootstrap"
	"github.com/pali-mem/pali/internal/config"
	"github.com/pali-mem/pali/internal/connect"
	"github.com/pali-mem/pali/internal/startup"
)

const (
	commandAPI  = "api"
	commandMCP  = "mcp"
	commandInit = "init"

	commandConnect = "connect"
)

var errHelp = errors.New("help requested")

type cliCommand struct {
	name    string
	cfgPath string
	init    bootstrap.Options
	tenant  string // mcp: -tenant value
	connect connect.Options
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

	if cmd.name == commandConnect {
		if err := runConnect(cmd.connect); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(cmd.cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch cmd.name {
	case commandMCP:
		runMCP(cfg, cmd.tenant)
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
	return fmt.Sprintf("http://%s:%d", host, port)
}

func runMCP(cfg config.Config, tenantFlag string) {
	tenantID, tenantName, err := resolveTenantFlag(tenantFlag, ".")
	if err != nil {
		log.Fatalf("resolve tenant: %v", err)
	}
	if tenantID != "" {
		cfg.DefaultTenantID = tenantID
	}
	runtime, err := startup.NewMCPRuntime(cfg)
	if err != nil {
		log.Fatalf("build mcp runtime: %v", err)
	}
	defer runtime.Cleanup()
	if tenantID != "" {
		if err := runtime.EnsureTenant(context.Background(), tenantID, tenantName); err != nil {
			runtime.Cleanup()
			log.Fatalf("ensure tenant: %v", err)
		}
		log.Printf("using tenant %q", tenantID)
	}

	log.Printf("starting pali mcp server over stdio")
	if err := runtime.Server.RunStdio(context.Background()); err != nil {
		log.Fatalf("mcp server exited: %v", err)
	}
}

// resolveTenantFlag turns the -tenant flag into a tenant ID and display name.
// "auto" derives both from the git repository or folder containing dir.
func resolveTenantFlag(flagValue, dir string) (id, name string, err error) {
	v := strings.TrimSpace(flagValue)
	switch v {
	case "":
		return "", "", nil
	case connect.AutoTenant:
		return connect.DeriveTenant(dir)
	}
	return v, v, nil
}

func runConnect(opts connect.Options) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate pali binary: %w", err)
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil && opts.Scope == connect.ScopeUser {
		return fmt.Errorf("locate home directory: %w", err)
	}
	opts.Exe, opts.Cwd, opts.Home, opts.Out = exe, cwd, home, os.Stdout
	return connect.Run(opts)
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

	if len(args) > 0 && args[0] == commandConnect {
		return parseConnectFlags(args[1:])
	}

	if len(args) > 0 && args[0] == "mcp" {
		return parseMCPFlags(trimServeToken(args[1:]))
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

func parseMCPFlags(args []string) (cliCommand, error) {
	fs := flag.NewFlagSet(commandMCP, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfgPath := fs.String("config", "pali.yaml", "Path to config file")
	tenant := fs.String("tenant", "", `Default tenant ID, created on startup if missing ("auto" derives it from the working directory)`)
	if err := fs.Parse(args); err != nil {
		return cliCommand{}, err
	}
	if fs.NArg() > 0 {
		return cliCommand{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	return cliCommand{name: commandMCP, cfgPath: *cfgPath, tenant: *tenant}, nil
}

func parseConnectFlags(args []string) (cliCommand, error) {
	var agent string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(commandConnect, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	scope := fs.String("scope", connect.ScopeProject, "Config scope: project or user")
	tenant := fs.String("tenant", "", "Tenant ID (default: derived from the git repo or directory name)")
	cfgPath := fs.String("config", "", "Pali config file the MCP server should load")
	dryRun := fs.Bool("dry-run", false, "Print what would change without writing")
	if err := fs.Parse(args); err != nil {
		return cliCommand{}, err
	}
	if agent == "" && fs.NArg() == 1 {
		agent = fs.Arg(0)
	} else if fs.NArg() > 0 {
		return cliCommand{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if agent == "" {
		return cliCommand{}, errors.New("connect requires an agent: claude-code, cursor or codex")
	}
	return cliCommand{name: commandConnect, connect: connect.Options{
		Agent:      agent,
		Scope:      *scope,
		Tenant:     *tenant,
		ConfigPath: *cfgPath,
		DryRun:     *dryRun,
	}}, nil
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
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  pali init [flags]                   # create config and run setup checks")
	_, _ = fmt.Fprintln(w, "  pali serve [-config <path>]         # start API server")
	_, _ = fmt.Fprintln(w, "  pali [-config <path>]               # start API server (default)")
	_, _ = fmt.Fprintln(w, "  pali api serve [-config <path>]     # start API server")
	_, _ = fmt.Fprintln(w, "  pali mcp serve [-config <path>] [-tenant <id|auto>]  # start MCP server over stdio")
	_, _ = fmt.Fprintln(w, "  pali mcp run [-config <path>]       # alias for mcp serve")
	_, _ = fmt.Fprintln(w, "  pali connect <claude-code|cursor|codex> [--scope project|user] [--tenant <id>] [--config <path>] [--dry-run]")
	_, _ = fmt.Fprintln(w, "                                      # add Pali to a coding agent's MCP config")
}
