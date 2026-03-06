package main

import "testing"

func TestParseArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     []string
		wantMode string
		wantCfg  string
		wantErr  bool
	}{
		{
			name:     "default api mode",
			args:     []string{},
			wantMode: modeAPI,
			wantCfg:  "pali.yaml",
		},
		{
			name:     "legacy api flags",
			args:     []string{"-config", "pali.yaml.example"},
			wantMode: modeAPI,
			wantCfg:  "pali.yaml.example",
		},
		{
			name:     "explicit api run",
			args:     []string{"api", "run", "-config", "/etc/pali.yaml"},
			wantMode: modeAPI,
			wantCfg:  "/etc/pali.yaml",
		},
		{
			name:     "explicit mcp run",
			args:     []string{"mcp", "run", "-config", "/etc/pali.yaml"},
			wantMode: modeMCP,
			wantCfg:  "/etc/pali.yaml",
		},
		{
			name:     "mcp alias without run",
			args:     []string{"mcp", "-config", "pali.yaml.example"},
			wantMode: modeMCP,
			wantCfg:  "pali.yaml.example",
		},
		{
			name:     "api run alias",
			args:     []string{"run", "-config", "pali.yaml.example"},
			wantMode: modeAPI,
			wantCfg:  "pali.yaml.example",
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
			mode, cfg, err := parseArgs(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != tc.wantMode {
				t.Fatalf("mode mismatch: got %q want %q", mode, tc.wantMode)
			}
			if cfg != tc.wantCfg {
				t.Fatalf("config mismatch: got %q want %q", cfg, tc.wantCfg)
			}
		})
	}
}
