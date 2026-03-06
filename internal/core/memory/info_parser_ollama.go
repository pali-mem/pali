package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/vein05/pali/internal/domain"
)

const (
	defaultParserOllamaBaseURL = "http://127.0.0.1:11434"
	defaultParserOllamaModel   = "deepseek-r1:7b"
	defaultParserOllamaTimeout = 20 * time.Second
)

type ollamaInfoParser struct {
	baseURL string
	model   string
	http    *http.Client
	logger  *log.Logger
	verbose bool
}

func NewOllamaInfoParser(baseURL, model string, timeout time.Duration, logger *log.Logger, verbose bool) (InfoParser, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultParserOllamaBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultParserOllamaModel
	}
	if timeout <= 0 {
		timeout = defaultParserOllamaTimeout
	}

	p := &ollamaInfoParser{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: timeout},
		logger:  logger,
		verbose: verbose,
	}
	if err := p.preflight(context.Background()); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *ollamaInfoParser) Parse(ctx context.Context, content string, maxFacts int) ([]ParsedFact, error) {
	if maxFacts <= 0 {
		return nil, fmt.Errorf("max facts must be > 0")
	}
	content = strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if content == "" {
		return []ParsedFact{}, nil
	}

	prompt := buildOllamaParserPrompt(content, maxFacts)
	start := time.Now()
	raw, err := p.generate(ctx, prompt)
	if err != nil {
		p.debugf("[pali-parser] model=%s status=error ms=%d err=%v", p.model, time.Since(start).Milliseconds(), err)
		return nil, err
	}
	parsed, err := decodeParserJSON(raw)
	if err != nil {
		p.debugf("[pali-parser] model=%s PARSE_ERROR raw_response=%q err=%v", p.model, sanitizeLogSnippet(raw, 260), err)
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
			entity, relation, value = inferEntityRelationValue(text, kind)
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
	p.debugf("[pali-parser] model=%s status=ok ms=%d facts=%d", p.model, time.Since(start).Milliseconds(), len(out))
	return out, nil
}

func (p *ollamaInfoParser) debugf(format string, args ...any) {
	if p == nil || p.logger == nil || !p.verbose {
		return
	}
	p.logger.Printf(format, args...)
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

func buildOllamaParserPrompt(content string, maxFacts int) string {
	return fmt.Sprintf(
		"Extract high-signal factual memories from one dialogue turn.\n"+
			"Rules:\n"+
			"1) Return JSON only, matching the schema exactly.\n"+
			"2) Exclude greetings, acknowledgements, compliments, chit-chat, standalone questions, and style-only text.\n"+
			"3) Keep each fact standalone, explicit, and non-ambiguous.\n"+
			"4) Resolve the subject whenever possible; avoid dangling pronouns like 'it', 'that', or 'do that'.\n"+
			"5) Prefer facts with a specific predicate and object/value.\n"+
			"6) If a fact is temporal, anchor it to an absolute or clearly provided time.\n"+
			"7) Preserve negation and constraints (e.g., 'does not', 'avoids').\n"+
			"8) Prefer concrete entities, preferences, commitments, plans, motivations, possessions, relationships, and dated events.\n"+
			"9) Do not invent or infer facts not present in the turn.\n"+
			"10) Output at most %d facts.\n"+
			"11) kind must be either observation or event.\n"+
			"12) If available, include entity/relation/value fields for aggregation lookups.\n"+
			"13) If no high-signal fact exists, return {\"facts\":[]}.\n"+
			"\n"+
			"JSON schema:\n"+
			"{\"facts\":[{\"content\":\"...\",\"kind\":\"observation|event\",\"tags\":[\"...\"],\"entity\":\"...\",\"relation\":\"...\",\"value\":\"...\"}]}\n"+
			"\n"+
			"Example:\n"+
			"Turn: \"Alice: I am vegetarian and avoid dairy\"\n"+
			"Output: {\"facts\":[{\"content\":\"Alice is vegetarian and avoids dairy.\",\"kind\":\"observation\",\"tags\":[\"preference\"],\"entity\":\"Alice\",\"relation\":\"activity\",\"value\":\"vegetarian and avoids dairy\"}]}\n"+
			"\n"+
			"Turn:\n%s",
		maxFacts,
		content,
	)
}

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

func (p *ollamaInfoParser) preflight(ctx context.Context) error {
	if _, err := p.do(ctx, http.MethodGet, "/api/version", nil); err != nil {
		return fmt.Errorf("connect to parser ollama at %s: %w", p.baseURL, err)
	}
	body, err := p.do(ctx, http.MethodGet, "/api/tags", nil)
	if err != nil {
		return fmt.Errorf("query parser ollama models: %w", err)
	}
	var parsed struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("decode parser ollama tags: %w", err)
	}
	if !containsOllamaModel(parsed.Models, p.model) {
		return fmt.Errorf("parser ollama model %q is not available", p.model)
	}
	return nil
}

func containsOllamaModel(models []struct {
	Name string `json:"name"`
}, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, m := range models {
		name := strings.ToLower(strings.TrimSpace(m.Name))
		if name == want || strings.HasPrefix(name, want+":") || strings.HasPrefix(want, name+":") {
			return true
		}
	}
	return false
}

func (p *ollamaInfoParser) generate(ctx context.Context, prompt string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"model":  p.model,
		"prompt": prompt,
		"stream": false,
		"format": "json",
		"options": map[string]any{
			"temperature": 0,
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal parser request: %w", err)
	}
	body, err := p.do(ctx, http.MethodPost, "/api/generate", payload)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Response string `json:"response"`
		Error    string `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode parser response: %w", err)
	}
	if strings.TrimSpace(parsed.Error) != "" {
		return "", fmt.Errorf("parser ollama error: %s", strings.TrimSpace(parsed.Error))
	}
	return strings.TrimSpace(parsed.Response), nil
}

func (p *ollamaInfoParser) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, p.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create parser request %s %s: %w", method, path, err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read parser response %s %s: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("parser ollama %s %s failed: %s", method, path, msg)
	}
	return respBody, nil
}
