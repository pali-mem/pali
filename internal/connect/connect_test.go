package connect

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// testExe must satisfy filepath.IsAbs on every OS; forward slashes keep the
// expected JSON and TOML strings free of escaped backslashes.
var testExe = func() string {
	if runtime.GOOS == "windows" {
		return "C:/opt/pali/bin/pali"
	}
	return "/opt/pali/bin/pali"
}()

// newProject makes a fake home and a git project dir named name.
func newProject(t *testing.T, name string) (home, project string) {
	t.Helper()
	base := t.TempDir()
	home = filepath.Join(base, "home")
	project = filepath.Join(base, name)
	for _, d := range []string{home, filepath.Join(project, ".git"), filepath.Join(project, "sub", "dir")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return home, project
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func jsonServers(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	var doc struct {
		MCPServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(readFile(t, path)), &doc); err != nil {
		t.Fatalf("invalid JSON in %s: %v", path, err)
	}
	return doc.MCPServers
}

func TestRunJSONAgents(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		scope string
		path  func(home, project string) string
	}{
		{"claude project", AgentClaudeCode, ScopeProject, func(_, p string) string { return filepath.Join(p, ".mcp.json") }},
		{"claude user", AgentClaudeCode, ScopeUser, func(h, _ string) string { return filepath.Join(h, ".claude.json") }},
		{"cursor project", AgentCursor, ScopeProject, func(_, p string) string { return filepath.Join(p, ".cursor", "mcp.json") }},
		{"cursor user", AgentCursor, ScopeUser, func(h, _ string) string { return filepath.Join(h, ".cursor", "mcp.json") }},
	}
	for _, tc := range tests {
		t.Run(tc.name+"/fresh file", func(t *testing.T) {
			home, project := newProject(t, "My Repo")
			path := tc.path(home, project)
			var out bytes.Buffer
			err := Run(Options{Agent: tc.agent, Scope: tc.scope, Home: home, Cwd: filepath.Join(project, "sub", "dir"), Exe: testExe, Out: &out})
			if err != nil {
				t.Fatal(err)
			}
			entry := jsonServers(t, path)[ServerName]
			if entry["command"] != testExe || entry["type"] != "stdio" {
				t.Fatalf("unexpected entry: %v", entry)
			}
			wantTenant := "my-repo"
			if tc.scope == ScopeUser {
				wantTenant = AutoTenant
			}
			want := []any{"mcp", "serve", "-tenant", wantTenant}
			if got, _ := json.Marshal(entry["args"]); string(got) != mustJSON(want) {
				t.Fatalf("args = %s, want %s", got, mustJSON(want))
			}
		})

		t.Run(tc.name+"/existing servers preserved", func(t *testing.T) {
			home, project := newProject(t, "repo")
			path := tc.path(home, project)
			writeFile(t, path, `{"numbers": 12345678901234567890, "theme": "dark", "mcpServers": {"other": {"command": "x", "args": ["a"]}}}`)
			err := Run(Options{Agent: tc.agent, Scope: tc.scope, Tenant: "custom", ConfigPath: "conf/pali.yaml", Home: home, Cwd: project, Exe: testExe})
			if err != nil {
				t.Fatal(err)
			}
			content := readFile(t, path)
			if !strings.Contains(content, "12345678901234567890") || !strings.Contains(content, `"theme": "dark"`) {
				t.Fatalf("unrelated keys lost:\n%s", content)
			}
			servers := jsonServers(t, path)
			if servers["other"]["command"] != "x" {
				t.Fatalf("other server lost: %v", servers)
			}
			wantArgs := mustJSON([]any{"mcp", "serve", "-config", filepath.Join(project, "conf", "pali.yaml"), "-tenant", "custom"})
			if got, _ := json.Marshal(servers[ServerName]["args"]); string(got) != wantArgs {
				t.Fatalf("args = %s, want %s", got, wantArgs)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			// Windows reports only read-only vs read-write, so 0o600 is not observable there.
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
				t.Fatalf("file mode changed to %v", info.Mode().Perm())
			}
		})

		t.Run(tc.name+"/idempotent rerun", func(t *testing.T) {
			home, project := newProject(t, "repo")
			path := tc.path(home, project)
			opts := Options{Agent: tc.agent, Scope: tc.scope, Home: home, Cwd: project, Exe: testExe}
			if err := Run(opts); err != nil {
				t.Fatal(err)
			}
			first := readFile(t, path)
			var out bytes.Buffer
			opts.Out = &out
			if err := Run(opts); err != nil {
				t.Fatal(err)
			}
			if readFile(t, path) != first {
				t.Fatal("second run changed the file")
			}
			if !strings.Contains(out.String(), "already configured") {
				t.Fatalf("output = %q", out.String())
			}
		})

		t.Run(tc.name+"/dry run writes nothing", func(t *testing.T) {
			home, project := newProject(t, "repo")
			path := tc.path(home, project)
			var out bytes.Buffer
			if err := Run(Options{Agent: tc.agent, Scope: tc.scope, DryRun: true, Home: home, Cwd: project, Exe: testExe, Out: &out}); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("dry run created %s (err=%v)", path, err)
			}
			if !strings.Contains(out.String(), "+++ "+path) || !strings.Contains(out.String(), `+    "pali"`) {
				t.Fatalf("diff missing expected lines:\n%s", out.String())
			}
		})
	}
}

func TestRunJSONInvalidExistingFileIsNotTouched(t *testing.T) {
	home, project := newProject(t, "repo")
	path := filepath.Join(project, ".mcp.json")
	writeFile(t, path, `{not json`)
	err := Run(Options{Agent: AgentClaudeCode, Home: home, Cwd: project, Exe: testExe})
	if err == nil {
		t.Fatal("expected error")
	}
	if readFile(t, path) != `{not json` {
		t.Fatal("invalid file was modified")
	}
}

func TestRunCodex(t *testing.T) {
	codexPath := func(home, project string, scope string) string {
		if scope == ScopeUser {
			return filepath.Join(home, ".codex", "config.toml")
		}
		return filepath.Join(project, ".codex", "config.toml")
	}
	wantBlock := "[mcp_servers.pali]\ncommand = \"" + testExe + "\"\nargs = [\"mcp\", \"serve\", \"-tenant\", \"repo\"]\n"

	for _, scope := range []string{ScopeProject, ScopeUser} {
		t.Run(scope+"/fresh file", func(t *testing.T) {
			home, project := newProject(t, "repo")
			tenant := "repo"
			if scope == ScopeUser {
				tenant = AutoTenant
			}
			if err := Run(Options{Agent: AgentCodex, Scope: scope, Home: home, Cwd: project, Exe: testExe}); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(wantBlock, `"repo"`, `"`+tenant+`"`, 1)
			if got := readFile(t, codexPath(home, project, scope)); got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}

	t.Run("other tables preserved and pali replaced", func(t *testing.T) {
		home, project := newProject(t, "repo")
		path := codexPath(home, project, ScopeProject)
		writeFile(t, path, `model = "o3"

# keep me
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

[mcp_servers.pali]
command = "/old/pali"
args = ["mcp", "serve"]

[mcp_servers.pali.env]
FOO = "bar"

[projects."/x"]
trust_level = "trusted"
`)
		if err := Run(Options{Agent: AgentCodex, Home: home, Cwd: project, Exe: testExe}); err != nil {
			t.Fatal(err)
		}
		want := `model = "o3"

# keep me
[mcp_servers.context7]
command = "npx"
args = ["-y", "@upstash/context7-mcp"]

` + wantBlock + `
[projects."/x"]
trust_level = "trusted"
`
		if got := readFile(t, path); got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("append to file without pali", func(t *testing.T) {
		home, project := newProject(t, "repo")
		path := codexPath(home, project, ScopeProject)
		writeFile(t, path, "[mcp_servers.other]\ncommand = \"x\"")
		if err := Run(Options{Agent: AgentCodex, Home: home, Cwd: project, Exe: testExe}); err != nil {
			t.Fatal(err)
		}
		want := "[mcp_servers.other]\ncommand = \"x\"\n\n" + wantBlock
		if got := readFile(t, path); got != want {
			t.Fatalf("got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("idempotent rerun and dry run", func(t *testing.T) {
		home, project := newProject(t, "repo")
		path := codexPath(home, project, ScopeProject)
		opts := Options{Agent: AgentCodex, Home: home, Cwd: project, Exe: testExe}
		if err := Run(opts); err != nil {
			t.Fatal(err)
		}
		first := readFile(t, path)
		var out bytes.Buffer
		opts.Out = &out
		if err := Run(opts); err != nil {
			t.Fatal(err)
		}
		if readFile(t, path) != first || !strings.Contains(out.String(), "already configured") {
			t.Fatalf("rerun not idempotent: %q", out.String())
		}
		// Dry run against a changed tenant must not write.
		out.Reset()
		opts.Tenant, opts.DryRun = "other", true
		if err := Run(opts); err != nil {
			t.Fatal(err)
		}
		if readFile(t, path) != first {
			t.Fatal("dry run modified the file")
		}
		if !strings.Contains(out.String(), `-    "repo"`) && !strings.Contains(out.String(), `-args = ["mcp", "serve", "-tenant", "repo"]`) {
			t.Fatalf("diff missing removal:\n%s", out.String())
		}
	})
}

func TestRunValidation(t *testing.T) {
	home, project := newProject(t, "repo")
	tests := []struct {
		name string
		opts Options
	}{
		{"unknown agent", Options{Agent: "vim", Home: home, Cwd: project, Exe: testExe}},
		{"bad scope", Options{Agent: AgentCursor, Scope: "global", Home: home, Cwd: project, Exe: testExe}},
		{"relative exe", Options{Agent: AgentCursor, Home: home, Cwd: project, Exe: "pali"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := Run(tc.opts); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDeriveTenant(t *testing.T) {
	base := t.TempDir()
	mk := func(rel string, git bool) string {
		dir := filepath.Join(base, rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if git {
			if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	repo := mk("My Cool.Repo", true)
	nested := mk("My Cool.Repo/pkg/x", false)
	plain := mk("plain_dir", false)
	worktreeFile := mk("wt", false)
	if err := os.WriteFile(filepath.Join(worktreeFile, ".git"), []byte("gitdir: /x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dots := mk("---", false)

	tests := []struct {
		name, dir, wantID, wantName string
		wantErr                     bool
	}{
		{"git root", repo, "my-cool-repo", "My Cool.Repo", false},
		{"nested dir uses git root", nested, "my-cool-repo", "My Cool.Repo", false},
		{"no git uses dir", plain, "plain_dir", "plain_dir", false},
		{".git file (worktree)", worktreeFile, "wt", "wt", false},
		{"unusable name", dots, "", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, name, err := DeriveTenant(tc.dir)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", id)
				}
				return
			}
			if err != nil || id != tc.wantID || name != tc.wantName {
				t.Fatalf("got (%q, %q, %v), want (%q, %q)", id, name, err, tc.wantID, tc.wantName)
			}
		})
	}
}

func TestSlugifyTenant(t *testing.T) {
	tests := map[string]string{
		"Pali":         "pali",
		"my repo":      "my-repo",
		"a..b//c":      "a-b-c",
		"__x__":        "__x__",
		"-lead-trail-": "lead-trail",
		"Ünï":          "n",
		"":             "",
	}
	for in, want := range tests {
		if got := SlugifyTenant(in); got != want {
			t.Errorf("SlugifyTenant(%q) = %q, want %q", in, got, want)
		}
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
