package memory

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

type parserResponse struct {
	Facts []struct {
		Content  string   `json:"content"`
		Kind     string   `json:"kind"`
		Tags     []string `json:"tags"`
		Entity   string   `json:"entity,omitempty"`
		Relation string   `json:"relation,omitempty"`
		Value    string   `json:"value,omitempty"`
	} `json:"facts"`
}

func parseParserFacts(raw string, maxFacts int) ([]ParsedFact, error) {
	if maxFacts <= 0 {
		return nil, fmt.Errorf("max facts must be > 0")
	}
	parsed, err := decodeParserJSON(raw)
	if err != nil {
		return nil, err
	}

	out := make([]ParsedFact, 0, maxFacts)
	seen := make(map[string]struct{}, maxFacts*2)
	for _, f := range parsed.Facts {
		text := strings.Join(strings.Fields(strings.TrimSpace(f.Content)), " ")
		if !isInformativeFact(text) {
			continue
		}
		kind := normalizeFactKind(f.Kind)
		entity := strings.Join(strings.Fields(strings.TrimSpace(f.Entity)), " ")
		relation := strings.Join(strings.Fields(strings.TrimSpace(f.Relation)), " ")
		value := strings.Join(strings.Fields(strings.TrimSpace(f.Value)), " ")
		if entity == "" || relation == "" || value == "" {
			inferredEntity, inferredRelation, inferredValue := inferEntityRelationValue(text, kind)
			if entity == "" {
				entity = inferredEntity
			}
			if relation == "" {
				relation = inferredRelation
			}
			if value == "" {
				value = inferredValue
			}
		}
		if entity == "" && (relation != "" || value != "") {
			entity = inferEntityFromFact(text)
			if entity == "" {
				entity = "user"
			}
		}
		key := strings.ToLower(text)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ParsedFact{
			Content:  text,
			Kind:     kind,
			Tags:     normalizeFactTags(f.Tags, kind),
			Entity:   entity,
			Relation: relation,
			Value:    value,
		})
		if len(out) >= maxFacts {
			break
		}
	}
	return out, nil
}

func normalizeFactKind(kind string) domain.MemoryKind {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case string(domain.MemoryKindEvent):
		return domain.MemoryKindEvent
	default:
		return domain.MemoryKindObservation
	}
}

func normalizeFactTags(tags []string, kind domain.MemoryKind) []string {
	base := append([]string{}, tags...)
	if kind == domain.MemoryKindEvent {
		base = append(base, "event", "parser")
	} else {
		base = append(base, "observation", "parser")
	}
	return mergeTags(nil, base...)
}

func decodeParserJSON(raw string) (parserResponse, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parserResponse{}, fmt.Errorf("empty parser response")
	}
	var parsed parserResponse
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		return parsed, nil
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return parserResponse{}, fmt.Errorf("parser returned non-JSON response")
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err != nil {
		return parserResponse{}, fmt.Errorf("decode parser JSON: %w", err)
	}
	return parsed, nil
}
