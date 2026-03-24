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

	coreprompts "github.com/pali-mem/pali/internal/core/prompts"
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

// NewOllamaInfoParser constructs an Ollama-backed info parser.
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

	prompt := coreprompts.Parser(content, maxFacts)
	start := time.Now()
	raw, err := p.generate(ctx, prompt)
	if err != nil {
		p.debugf("[pali-parser] model=%s status=error ms=%d err=%v", p.model, time.Since(start).Milliseconds(), err)
		return nil, err
	}
	parsed, err := parseParserFacts(raw, maxFacts)
	if err != nil {
		p.debugf("[pali-parser] model=%s PARSE_ERROR raw_response=%q err=%v", p.model, sanitizeLogSnippet(raw, 260), err)
		return nil, err
	}
	p.debugf("[pali-parser] model=%s status=ok ms=%d facts=%d", p.model, time.Since(start).Milliseconds(), len(parsed))
	return parsed, nil
}

func (p *ollamaInfoParser) debugf(format string, args ...any) {
	if p == nil || p.logger == nil || !p.verbose {
		return
	}
	p.logger.Printf(format, args...)
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
	defer func() {
		_ = resp.Body.Close()
	}()
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
