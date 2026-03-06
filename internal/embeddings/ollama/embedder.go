package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const (
	defaultBaseURL = "http://127.0.0.1:11434"
	defaultModel   = "all-minilm"
	defaultTimeout = 10 * time.Second
)

type Embedder struct {
	baseURL string
	model   string
	client  *http.Client
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

type embedRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

type ollamaError struct {
	Error string `json:"error"`
}

func NewEmbedder(baseURL, model string, timeout time.Duration) (*Embedder, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultModel
	}

	if timeout <= 0 {
		timeout = defaultTimeout
	}

	e := &Embedder{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: timeout},
	}
	if err := e.preflight(context.Background()); err != nil {
		return nil, err
	}
	return e, nil
}

func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := e.BatchEmbed(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return embeddings[0], nil
}

func (e *Embedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	if e == nil {
		return nil, fmt.Errorf("ollama embedder is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	reqBody := embedRequest{
		Model: e.model,
		Input: texts,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal ollama embed request: %w", err)
	}

	respBody, err := e.do(ctx, http.MethodPost, "/api/embed", body)
	if err != nil {
		return nil, err
	}

	var parsed embedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode ollama embed response: %w", err)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed response count mismatch: got %d embeddings for %d inputs", len(parsed.Embeddings), len(texts))
	}
	for i := range parsed.Embeddings {
		if len(parsed.Embeddings[i]) == 0 {
			return nil, fmt.Errorf("ollama embed response has empty embedding at index %d", i)
		}
	}
	if len(parsed.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama embed response did not include embeddings")
	}
	return parsed.Embeddings, nil
}

func (e *Embedder) preflight(ctx context.Context) error {
	if _, err := e.do(ctx, http.MethodGet, "/api/version", nil); err != nil {
		return fmt.Errorf("connect to ollama at %s: %w\n%s", e.baseURL, err, ollamaSetupHint(e.model))
	}

	respBody, err := e.do(ctx, http.MethodGet, "/api/tags", nil)
	if err != nil {
		return fmt.Errorf("query ollama models at %s: %w\n%s", e.baseURL, err, ollamaSetupHint(e.model))
	}

	var tags tagsResponse
	if err := json.Unmarshal(respBody, &tags); err != nil {
		return fmt.Errorf("decode ollama model list: %w", err)
	}
	if !hasModel(tags.Models, e.model) {
		return fmt.Errorf("ollama model %q is not available\n%s", e.model, ollamaPullHint(e.model))
	}
	return nil
}

func hasModel(models []struct {
	Name string `json:"name"`
}, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	for _, m := range models {
		name := strings.TrimSpace(strings.ToLower(m.Name))
		if name == want {
			return true
		}
		if strings.HasPrefix(name, want+":") {
			return true
		}
		if strings.HasPrefix(want, name+":") {
			return true
		}
	}
	return false
}

func (e *Embedder) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	url := e.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request %s %s: %w", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response %s %s: %w", method, url, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		var apiErr ollamaError
		if err := json.Unmarshal(respBody, &apiErr); err == nil && strings.TrimSpace(apiErr.Error) != "" {
			msg = strings.TrimSpace(apiErr.Error)
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("ollama %s %s failed: %s", method, path, msg)
	}

	return respBody, nil
}

func ollamaSetupHint(model string) string {
	var b strings.Builder
	switch runtime.GOOS {
	case "darwin":
		b.WriteString("Install Ollama (macOS): brew install --cask ollama or https://ollama.com/download\n")
	case "windows":
		b.WriteString("Install Ollama (Windows): https://ollama.com/download\n")
	default:
		b.WriteString("Install Ollama: https://ollama.com/download\n")
	}
	b.WriteString("Then run:\n")
	b.WriteString("  1) ollama serve\n")
	b.WriteString("  2) ollama pull ")
	b.WriteString(strings.TrimSpace(model))
	return b.String()
}

func ollamaPullHint(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		model = defaultModel
	}
	return "Download it with: ollama pull " + model
}
