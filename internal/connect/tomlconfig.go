package connect

import (
	"regexp"
	"strings"
)

var tomlTableHeader = regexp.MustCompile(`^\s*\[([^\[\]]+)\]\s*(#.*)?$`)

// isServerTable reports whether line is the header of [mcp_servers.<name>] or
// one of its sub-tables (for example [mcp_servers.pali.env]).
func isServerTable(line, name string) bool {
	m := tomlTableHeader.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	segs := strings.Split(m[1], ".")
	if len(segs) < 2 {
		return false
	}
	clean := func(s string) string { return strings.Trim(strings.TrimSpace(s), `"'`) }
	return clean(segs[0]) == "mcp_servers" && clean(segs[1]) == name
}

func isAnyTableHeader(line string) bool {
	t := strings.TrimSpace(line)
	return tomlTableHeader.MatchString(line) || strings.HasPrefix(t, "[[")
}

func tomlStringArray(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = jsonString(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// mergeMCPServerTOML adds or replaces the [mcp_servers.<name>] table (and its
// sub-tables) in data, leaving every other line untouched. changed is false
// when the existing table text already equals the generated one.
func mergeMCPServerTOML(data []byte, name, command string, args []string) (out []byte, changed bool) {
	block := []string{
		"[mcp_servers." + name + "]",
		"command = " + jsonString(command),
		"args = " + tomlStringArray(args),
	}

	var kept, existing []string
	inServer := false
	insertAt := -1
	for _, line := range splitLines(string(data)) {
		switch {
		case isServerTable(line, name):
			inServer = true
			if insertAt < 0 {
				insertAt = len(kept)
			}
			existing = append(existing, line)
		case inServer && isAnyTableHeader(line):
			inServer = false
			kept = append(kept, line)
		case inServer:
			existing = append(existing, line)
		default:
			kept = append(kept, line)
		}
	}

	if insertAt >= 0 {
		if equalTrimmed(existing, block) {
			return data, false
		}
		repl := append([]string{}, block...)
		if insertAt < len(kept) {
			repl = append(repl, "") // blank line before the next table
		}
		merged := append(append(append([]string{}, kept[:insertAt]...), repl...), kept[insertAt:]...)
		return []byte(strings.Join(merged, "\n") + "\n"), true
	}

	merged := kept
	if len(merged) > 0 && strings.TrimSpace(merged[len(merged)-1]) != "" {
		merged = append(merged, "")
	}
	merged = append(merged, block...)
	return []byte(strings.Join(merged, "\n") + "\n"), true
}

func equalTrimmed(a, b []string) bool {
	trim := func(in []string) []string {
		var out []string
		for _, l := range in {
			if t := strings.TrimSpace(l); t != "" {
				out = append(out, t)
			}
		}
		return out
	}
	x, y := trim(a), trim(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
