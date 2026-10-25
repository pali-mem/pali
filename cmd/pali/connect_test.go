package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pali-mem/pali/internal/connect"
)

func TestParseArgsMCPTenantAndConnect(t *testing.T) {
	t.Parallel()

	cmd, err := parseArgs([]string{"mcp", "serve", "-config", "/c.yaml", "-tenant", "auto"})
	if err != nil || cmd.name != commandMCP || cmd.cfgPath != "/c.yaml" || cmd.tenant != "auto" {
		t.Fatalf("mcp serve -tenant: cmd=%+v err=%v", cmd, err)
	}
	if _, err := parseArgs([]string{"serve", "-tenant", "x"}); err == nil {
		t.Fatal("-tenant must be rejected outside mcp")
	}

	tests := []struct {
		name    string
		args    []string
		want    connect.Options
		wantErr bool
	}{
		{"agent only", []string{"connect", "cursor"}, connect.Options{Agent: "cursor", Scope: "project"}, false},
		{"flags after agent", []string{"connect", "codex", "--scope", "user", "--tenant", "t", "--config", "p.yaml", "--dry-run"},
			connect.Options{Agent: "codex", Scope: "user", Tenant: "t", ConfigPath: "p.yaml", DryRun: true}, false},
		{"flags before agent", []string{"connect", "--dry-run", "claude-code"}, connect.Options{Agent: "claude-code", Scope: "project", DryRun: true}, false},
		{"missing agent", []string{"connect"}, connect.Options{}, true},
		{"extra positional", []string{"connect", "cursor", "extra"}, connect.Options{}, true},
		{"unknown flag", []string{"connect", "cursor", "--nope"}, connect.Options{}, true},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd, err := parseArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cmd.name != commandConnect || cmd.connect != tc.want {
				t.Fatalf("got %+v, want %+v", cmd.connect, tc.want)
			}
		})
	}
}

func TestResolveTenantFlag(t *testing.T) {
	t.Parallel()

	repo := filepath.Join(t.TempDir(), "My Repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		flag, wantID, wantName string
	}{
		{"", "", ""},
		{"  ", "", ""},
		{"explicit", "explicit", "explicit"},
		{"auto", "my-repo", "My Repo"},
	}
	for _, tc := range tests {
		id, name, err := resolveTenantFlag(tc.flag, repo)
		if err != nil || id != tc.wantID || name != tc.wantName {
			t.Errorf("resolveTenantFlag(%q) = (%q, %q, %v), want (%q, %q)", tc.flag, id, name, err, tc.wantID, tc.wantName)
		}
	}
}
