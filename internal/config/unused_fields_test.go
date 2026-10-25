package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// unreferencedAllowList lists Config leaf fields (dotted Go path) that no
// non-config, non-test Go file reads, with the reason each is tolerated.
// Remove an entry when the field is wired up or deleted.
var unreferencedAllowList = map[string]string{}

// TestConfigFieldsAreReferencedOutsideConfig fails when a leaf field of Config
// is never selected (x.Field) in any Go file of the module outside
// internal/config and *_test.go files.
//
// Matching is by selector name only (no type information), so a field whose
// name is shared with an unrelated struct can count as referenced when it is
// not. The check can miss dead fields but should not flag live ones.
func TestConfigFieldsAreReferencedOutsideConfig(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	selectors := collectSelectorNames(t, root)

	var unreferenced []string
	seenAllow := map[string]bool{}
	for _, leaf := range configLeafFields(reflect.TypeOf(Config{}), "") {
		name := leaf[strings.LastIndex(leaf, ".")+1:]
		if _, ok := unreferencedAllowList[leaf]; ok {
			seenAllow[leaf] = true
			if selectors[name] {
				t.Errorf("allow-list entry %q is referenced outside internal/config; remove it", leaf)
			}
			continue
		}
		if !selectors[name] {
			unreferenced = append(unreferenced, leaf)
		}
	}
	for leaf := range unreferencedAllowList {
		if !seenAllow[leaf] {
			t.Errorf("allow-list entry %q does not match any Config field; remove it", leaf)
		}
	}
	if len(unreferenced) > 0 {
		sort.Strings(unreferenced)
		t.Errorf("Config fields never read outside internal/config (wire them up, delete them, or add to unreferencedAllowList with a reason):\n  %s",
			strings.Join(unreferenced, "\n  "))
	}
}

// configLeafFields returns dotted Go paths of all non-struct fields under t.
func configLeafFields(t reflect.Type, prefix string) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		path := prefix + f.Name
		if f.Type.Kind() == reflect.Struct {
			out = append(out, configLeafFields(f.Type, path+".")...)
			continue
		}
		out = append(out, path)
	}
	return out
}

// collectSelectorNames parses every non-test Go file outside internal/config
// and returns the set of identifiers used as the selected name in x.Name.
func collectSelectorNames(t *testing.T, root string) map[string]bool {
	t.Helper()
	configDir := filepath.Join(root, "internal", "config")
	names := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "vendor", "node_modules", ".claude":
				return filepath.SkipDir
			}
			if path == configDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				names[sel.Sel.Name] = true
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan module: %v", err)
	}
	return names
}
