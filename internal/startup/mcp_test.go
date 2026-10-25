package startup

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pali-mem/pali/internal/config"
)

func TestMCPRuntimeEnsureTenant(t *testing.T) {
	cfg := config.Defaults()
	cfg.Embedding.Provider = "lexical"
	cfg.Postprocess.Enabled = false
	cfg.Database.SQLiteDSN = filepath.Join(t.TempDir(), "pali.sqlite")

	rt, err := NewMCPRuntime(cfg)
	if err != nil {
		t.Fatalf("NewMCPRuntime: %v", err)
	}
	defer rt.Cleanup()

	ctx := context.Background()
	for i := 0; i < 2; i++ { // second call must be a no-op, not a conflict
		if err := rt.EnsureTenant(ctx, "my-repo", "My Repo"); err != nil {
			t.Fatalf("EnsureTenant call %d: %v", i+1, err)
		}
	}
	exists, err := rt.tenants.Exists(ctx, "my-repo")
	if err != nil || !exists {
		t.Fatalf("tenant not created: exists=%v err=%v", exists, err)
	}
	tenants, err := rt.tenants.List(ctx, 10)
	if err != nil || len(tenants) != 1 || tenants[0].Name != "My Repo" {
		t.Fatalf("unexpected tenants: %+v err=%v", tenants, err)
	}

	if err := rt.EnsureTenant(ctx, "bare", ""); err != nil {
		t.Fatal(err)
	}
	tenants, _ = rt.tenants.List(ctx, 10)
	for _, tn := range tenants {
		if tn.ID == "bare" && tn.Name != "bare" {
			t.Fatalf("empty name should fall back to id, got %q", tn.Name)
		}
	}
}
