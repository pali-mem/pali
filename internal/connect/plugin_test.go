package connect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pluginDir = "../../integrations/claude-code"

func readPluginJSON(t *testing.T, rel string, v any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(pluginDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
}

func TestClaudeCodePluginFiles(t *testing.T) {
	var manifest struct {
		Name string `json:"name"`
	}
	readPluginJSON(t, ".claude-plugin/plugin.json", &manifest)
	if manifest.Name != "pali-memory" {
		t.Fatalf("plugin name = %q", manifest.Name)
	}

	var mcp struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	readPluginJSON(t, ".mcp.json", &mcp)
	srv, ok := mcp.MCPServers[ServerName]
	if !ok || srv.Command != "pali" {
		t.Fatalf("unexpected mcp servers: %+v", mcp.MCPServers)
	}
	if got := strings.Join(srv.Args, " "); !strings.HasPrefix(got, "mcp serve") || !strings.HasSuffix(got, "-tenant "+AutoTenant) {
		t.Fatalf("args = %q", got)
	}

	var hooks struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type string   `json:"type"`
				Args []string `json:"args"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	readPluginJSON(t, "hooks/hooks.json", &hooks)
	start := hooks.Hooks["SessionStart"]
	if len(start) != 1 || len(start[0].Hooks) != 1 || start[0].Hooks[0].Type != "command" {
		t.Fatalf("unexpected SessionStart hooks: %+v", hooks.Hooks)
	}
	protocolRef := start[0].Hooks[0].Args[0]
	if !strings.HasSuffix(protocolRef, "/hooks/memory-protocol.txt") {
		t.Fatalf("hook does not print the protocol file: %q", protocolRef)
	}

	text, err := os.ReadFile(filepath.Join(pluginDir, "hooks", "memory-protocol.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Split(strings.TrimSpace(string(text)), "\n")); n > 15 {
		t.Fatalf("protocol is %d lines, want at most 15", n)
	}
	for _, want := range []string{"memory_search", "memory_store", "Never store secrets"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("protocol missing %q", want)
		}
	}
}
