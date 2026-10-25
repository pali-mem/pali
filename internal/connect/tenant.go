// Package connect writes Pali MCP server entries into coding-agent configs.
package connect

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// AutoTenant is the `-tenant` value that asks `pali mcp serve` to derive the
// tenant from its working directory at startup.
const AutoTenant = "auto"

// FindProjectRoot returns the nearest ancestor of dir (including dir) that
// contains a .git entry, or dir itself when none exists.
func FindProjectRoot(dir string) string {
	for cur := dir; ; {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return dir
		}
		cur = parent
	}
}

// DeriveTenant returns a tenant ID and display name derived from the git
// repository (or directory) that contains dir.
func DeriveTenant(dir string) (id, name string, err error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", err
	}
	name = filepath.Base(FindProjectRoot(abs))
	id = SlugifyTenant(name)
	if id == "" {
		return "", "", errors.New("cannot derive a tenant name from " + abs + "; pass --tenant")
	}
	return id, name, nil
}

// SlugifyTenant lowercases s and collapses every run of characters outside
// [a-z0-9_] into a single hyphen.
func SlugifyTenant(s string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
			continue
		}
		pendingDash = true
	}
	return b.String()
}
