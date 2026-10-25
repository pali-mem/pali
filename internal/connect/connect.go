package connect

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Supported agent identifiers.
const (
	AgentClaudeCode = "claude-code"
	AgentCursor     = "cursor"
	AgentCodex      = "codex"
)

// Supported scopes.
const (
	ScopeProject = "project"
	ScopeUser    = "user"
)

// ServerName is the MCP server key written into agent configs.
const ServerName = "pali"

// Options configures Run. Home, Cwd and Exe are injected so callers (and tests)
// control every path that gets touched.
type Options struct {
	Agent      string
	Scope      string // ScopeProject (default) or ScopeUser
	Tenant     string // explicit tenant ID; empty derives one (see Run)
	ConfigPath string // optional pali config path, made absolute
	DryRun     bool

	Home string // user home directory
	Cwd  string // working directory used for project scope and tenant derivation
	Exe  string // absolute path of the pali binary
	Out  io.Writer
}

// Run writes the Pali MCP server entry for opts.Agent and reports the result on
// opts.Out. With DryRun it prints a diff and writes nothing.
//
// Tenant: --tenant wins. Otherwise project scope pins the tenant derived from
// the git repository/directory name, and user scope passes `-tenant auto` so
// each project resolves its own tenant when the server starts.
func Run(opts Options) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Scope == "" {
		opts.Scope = ScopeProject
	}
	if opts.Scope != ScopeProject && opts.Scope != ScopeUser {
		return fmt.Errorf("invalid scope %q (want project or user)", opts.Scope)
	}
	if !filepath.IsAbs(opts.Exe) {
		return fmt.Errorf("pali executable path must be absolute, got %q", opts.Exe)
	}

	args, tenantNote, err := serveArgs(opts)
	if err != nil {
		return err
	}
	path, err := targetPath(opts)
	if err != nil {
		return err
	}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}

	var updated []byte
	var changed bool
	switch opts.Agent {
	case AgentClaudeCode, AgentCursor:
		// Both vendors use {"mcpServers": {"<name>": {"type","command","args"}}}.
		entry := map[string]any{"type": "stdio", "command": opts.Exe, "args": args}
		updated, changed, err = mergeMCPServerJSON(existing, ServerName, entry)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	case AgentCodex:
		updated, changed = mergeMCPServerTOML(existing, ServerName, opts.Exe, args)
	}

	if !changed {
		_, _ = fmt.Fprintf(opts.Out, "already configured: %s (no changes)\n", path)
		return nil
	}
	if opts.DryRun {
		_, _ = fmt.Fprintf(opts.Out, "dry run: would update %s (tenant: %s)\n", path, tenantNote)
		writeDiff(opts.Out, path, existing, updated)
		return nil
	}
	if err := writeFileAtomic(path, updated); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	_, _ = fmt.Fprintf(opts.Out, "updated %s (tenant: %s)\n", path, tenantNote)
	return nil
}

func serveArgs(opts Options) (args []string, tenantNote string, err error) {
	args = []string{"mcp", "serve"}
	if cfg := strings.TrimSpace(opts.ConfigPath); cfg != "" {
		abs := cfg
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(opts.Cwd, abs)
		}
		args = append(args, "-config", filepath.Clean(abs))
	}
	tenant := strings.TrimSpace(opts.Tenant)
	switch {
	case tenant != "":
		tenantNote = tenant
	case opts.Scope == ScopeUser:
		tenant = AutoTenant
		tenantNote = "auto (derived from the project directory at startup)"
	default:
		id, _, derr := DeriveTenant(opts.Cwd)
		if derr != nil {
			return nil, "", derr
		}
		tenant = id
		tenantNote = id
	}
	return append(args, "-tenant", tenant), tenantNote, nil
}

// targetPath returns the config file for the agent and scope.
func targetPath(opts Options) (string, error) {
	if opts.Scope == ScopeUser && opts.Home == "" {
		return "", fmt.Errorf("home directory is required for user scope")
	}
	root := FindProjectRoot(opts.Cwd)
	switch opts.Agent {
	case AgentClaudeCode:
		// Project scope: .mcp.json at the project root; user scope: top-level
		// "mcpServers" in ~/.claude.json.
		// https://code.claude.com/docs/en/mcp
		if opts.Scope == ScopeUser {
			return filepath.Join(opts.Home, ".claude.json"), nil
		}
		return filepath.Join(root, ".mcp.json"), nil
	case AgentCursor:
		// https://cursor.com/docs/context/mcp (docs.cursor.com redirects here)
		if opts.Scope == ScopeUser {
			return filepath.Join(opts.Home, ".cursor", "mcp.json"), nil
		}
		return filepath.Join(root, ".cursor", "mcp.json"), nil
	case AgentCodex:
		// [mcp_servers.<name>] in config.toml; project-level .codex/config.toml
		// is honored only for trusted projects.
		// https://developers.openai.com/codex/mcp
		if opts.Scope == ScopeUser {
			return filepath.Join(opts.Home, ".codex", "config.toml"), nil
		}
		return filepath.Join(root, ".codex", "config.toml"), nil
	}
	return "", fmt.Errorf("unknown agent %q (want claude-code, cursor or codex)", opts.Agent)
}
