package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL = "http://127.0.0.1:11434"
	defaultModel   = "deepseek-r1:7b"
	defaultTimeout = 2 * time.Second
)

type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

type ollamaError struct {
	Error string `json:"error"`
}

func NewClient(baseURL, model string, timeout time.Duration) (*Client, error) {
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

	c := &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}
	if err := c.preflight(context.Background()); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) Model() string { return c.model }

func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("ollama scorer client is nil")
	}
	payload, err := json.Marshal(map[string]any{
		"model":  c.model,
		"prompt": prompt,
		"stream": false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal ollama generate request: %w", err)
	}

	body, err := c.do(ctx, http.MethodPost, "/api/generate", payload)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode ollama generate response: %w", err)
	}
	return strings.TrimSpace(parsed.Response), nil
}

func (c *Client) preflight(ctx context.Context) error {
	if _, err := c.do(ctx, http.MethodGet, "/api/version", nil); err != nil {
		return fmt.Errorf("connect to ollama at %s: %w", c.baseURL, err)
	}

	respBody, err := c.do(ctx, http.MethodGet, "/api/tags", nil)
	if err != nil {
		return fmt.Errorf("query ollama models at %s: %w", c.baseURL, err)
	}

	var tags tagsResponse
	if err := json.Unmarshal(respBody, &tags); err != nil {
		return fmt.Errorf("decode ollama model list: %w", err)
	}
	if !hasModel(tags.Models, c.model) {
		return fmt.Errorf("ollama model %q is not available", c.model)
	}
	return nil
}

func hasModel(models []struct {
	Name string `json:"name"`
}, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	for _, m := range models {
		name := strings.TrimSpace(strings.ToLower(m.Name))
		if name == want || strings.HasPrefix(name, want+":") || strings.HasPrefix(want, name+":") {
			return true
		}
	}
	return false
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request %s %s: %w", method, url, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
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
