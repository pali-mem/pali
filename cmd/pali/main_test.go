package main

import "testing"

func TestParseArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantMode string
		wantCfg  string
		wantInit bool
		wantErr  bool
	}{
		{
			name:     "default api mode",
			args:     []string{},
			wantMode: commandAPI,
			wantCfg:  "pali.yaml",
		},
		{
			name:     "legacy api flags",
			args:     []string{"-config", "pali.yaml.example"},
			wantMode: commandAPI,
			wantCfg:  "pali.yaml.example",
		},
		{
			name:     "explicit api serve",
			args:     []string{"api", "serve", "-config", "/etc/pali.yaml"},
			wantMode: commandAPI,
			wantCfg:  "/etc/pali.yaml",
		},
		{
			name:     "explicit mcp run",
			args:     []string{"mcp", "run", "-config", "/etc/pali.yaml"},
			wantMode: commandMCP,
			wantCfg:  "/etc/pali.yaml",
		},
		{
			name:     "mcp alias without run",
			args:     []string{"mcp", "-config", "pali.yaml.example"},
			wantMode: commandMCP,
			wantCfg:  "pali.yaml.example",
		},
		{
			name:     "api serve alias",
			args:     []string{"serve", "-config", "pali.yaml.example"},
			wantMode: commandAPI,
			wantCfg:  "pali.yaml.example",
		},
		{
			name:     "init command",
			args:     []string{"init", "-config", "configs/dev.yaml", "-skip-ollama-check"},
			wantMode: commandInit,
			wantCfg:  "configs/dev.yaml",
			wantInit: true,
		},
		{
			name:    "unexpected positional in mcp",
			args:    []string{"mcp", "run", "oops"},
			wantErr: true,
		},
		{
			name:    "invalid flag",
			args:    []string{"mcp", "run", "--unknown"},
			wantErr: true,
		},
		{
			name:    "help token",
			args:    []string{"help"},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd, err := parseArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cmd.name != tc.wantMode {
				t.Fatalf("mode mismatch: got %q want %q", cmd.name, tc.wantMode)
			}
			cfg := cmd.cfgPath
			if tc.wantInit {
				cfg = cmd.init.ConfigPath
			}
			if cfg != tc.wantCfg {
				t.Fatalf("config mismatch: got %q want %q", cfg, tc.wantCfg)
			}
		})
	}
}
