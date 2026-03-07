package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultTimeout = 15 * time.Second

// Option configures a Client at construction time.
type Option func(*Client)

// MemoryClient is the interface implemented by *Client for memory operations.
// Callers should use this interface as a parameter type to allow mocking in tests:
//
//	type MyService struct { mem client.MemoryClient }
type MemoryClient interface {
	StoreMemory(ctx context.Context, req StoreMemoryRequest) (StoreMemoryResponse, error)
	StoreMemoryBatch(ctx context.Context, req StoreMemoryBatchRequest) (StoreMemoryBatchResponse, error)
	SearchMemory(ctx context.Context, req SearchMemoryRequest) (SearchMemoryResponse, error)
	DeleteMemory(ctx context.Context, tenantID, memoryID string) error
}

// Client is a typed HTTP client for the Pali API.
// It is safe to use concurrently from multiple goroutines.
type Client struct {
	baseURL     *url.URL
	httpClient  *http.Client
	bearerToken string
}

// Ensure *Client satisfies MemoryClient at compile time.
var _ MemoryClient = (*Client)(nil)

// New is an alias of NewClient.
func New(baseURL string, opts ...Option) (*Client, error) {
	return NewClient(baseURL, opts...)
}

// NewClient constructs a new API client for the provided base URL.
func NewClient(baseURL string, opts ...Option) (*Client, error) {
	parsed, err := parseBaseURL(baseURL)
	if err != nil {
		return nil, err
	}

	c := &Client{
		baseURL:    parsed,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(c)
		}
	}

	if c.httpClient == nil {
		c.httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return c, nil
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithBearerToken sets a default bearer token on the client.
func WithBearerToken(token string) Option {
	return func(c *Client) {
		c.bearerToken = strings.TrimSpace(token)
	}
}

// SetBearerToken updates the bearer token used for subsequent requests.
func (c *Client) SetBearerToken(token string) {
	c.bearerToken = strings.TrimSpace(token)
}

func parseBaseURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("base URL is required")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("base URL must include scheme and host")
	}

	if !strings.HasSuffix(u.Path, "/") {
		u.Path += "/"
	}
	return u, nil
}
