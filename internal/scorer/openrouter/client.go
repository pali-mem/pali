// Package openrouter provides OpenRouter-backed importance scoring.
package openrouter

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
	defaultBaseURL = "https://openrouter.ai/api/v1"
	defaultTimeout = 10 * time.Second
)

// Client wraps the OpenRouter chat-completions API.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

type apiErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

// NewClient constructs an OpenRouter client.
func NewClient(baseURL, apiKey, model string, timeout time.Duration) (*Client, error) {
	baseURL = strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openrouter api key is required")
	}

	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("openrouter scoring model is required")
	}

	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}, nil
}

// Model returns the configured model name.
func (c *Client) Model() string { return c.model }

// Generate submits a prompt and returns the generated text.
func (c *Client) Generate(ctx context.Context, prompt string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("openrouter scorer client is nil")
	}

	payload, err := json.Marshal(map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"temperature": 0,
	})
	if err != nil {
		return "", fmt.Errorf("marshal openrouter chat request: %w", err)
	}

	body, err := c.do(ctx, http.MethodPost, "/chat/completions", payload)
	if err != nil {
		return "", err
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("decode openrouter chat response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("openrouter chat response had no choices")
	}

	content := extractMessageContent(parsed.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("openrouter chat response content is empty")
	}
	return content, nil
}

func extractMessageContent(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			text, _ := m["text"].(string)
			text = strings.TrimSpace(text)
			if text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, ""))
	default:
		return ""
	}
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
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response %s %s: %w", method, url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(respBody))
		var apiErr apiErrorResponse
		if err := json.Unmarshal(respBody, &apiErr); err == nil {
			if strings.TrimSpace(apiErr.Error.Message) != "" {
				msg = strings.TrimSpace(apiErr.Error.Message)
			} else if strings.TrimSpace(apiErr.Message) != "" {
				msg = strings.TrimSpace(apiErr.Message)
			}
		}
		if msg == "" {
			msg = resp.Status
		}
		return nil, fmt.Errorf("openrouter %s %s failed: %s", method, path, msg)
	}

	return respBody, nil
}
