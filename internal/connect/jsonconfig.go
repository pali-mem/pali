package connect

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// mergeMCPServerJSON sets mcpServers[name] = entry in the JSON document data
// (empty data means a new document). Unrelated keys are preserved; key order
// and whitespace are normalized. changed is false when the entry is already
// present with identical content.
func mergeMCPServerJSON(data []byte, name string, entry map[string]any) (out []byte, changed bool, err error) {
	doc := map[string]any{}
	if len(bytes.TrimSpace(data)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber() // keep large integers in files such as ~/.claude.json intact
		if err := dec.Decode(&doc); err != nil {
			return nil, false, fmt.Errorf("parse existing JSON: %w", err)
		}
	}
	servers := map[string]any{}
	if raw, ok := doc["mcpServers"]; ok && raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf(`existing "mcpServers" is not an object`)
		}
		servers = m
	}
	// Normalize entry through JSON so it compares equal to a decoded document.
	norm, err := normalizeJSON(entry)
	if err != nil {
		return nil, false, err
	}
	if cur, ok := servers[name]; ok && reflect.DeepEqual(cur, norm) {
		return data, false, nil
	}
	servers[name] = norm
	doc["mcpServers"] = servers

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

func normalizeJSON(v map[string]any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// jsonString quotes s as a JSON string; the result is also a valid TOML basic string.
func jsonString(s string) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(sb.String(), "\n")
}
