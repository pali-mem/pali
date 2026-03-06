package client

import (
	"context"
	"net/http"
)

// Health calls GET /health.
func (c *Client) Health(ctx context.Context) (HealthResponse, error) {
	var out HealthResponse
	err := c.doJSON(ctx, http.MethodGet, "/health", nil, nil, &out)
	return out, err
}
