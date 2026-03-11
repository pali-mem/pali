package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// StoreMemory calls POST /v1/memory.
func (c *Client) StoreMemory(ctx context.Context, req StoreMemoryRequest) (StoreMemoryResponse, error) {
	var out StoreMemoryResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/memory", nil, req, &out)
	return out, err
}

// StoreMemoryBatch calls POST /v1/memory/batch.
func (c *Client) StoreMemoryBatch(ctx context.Context, req StoreMemoryBatchRequest) (StoreMemoryBatchResponse, error) {
	var out StoreMemoryBatchResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/memory/batch", nil, req, &out)
	return out, err
}

// SearchMemory calls POST /v1/memory/search.
func (c *Client) SearchMemory(ctx context.Context, req SearchMemoryRequest) (SearchMemoryResponse, error) {
	var out SearchMemoryResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/memory/search", nil, req, &out)
	return out, err
}

// DeleteMemory calls DELETE /v1/memory/:id?tenant_id=...
func (c *Client) DeleteMemory(ctx context.Context, tenantID, memoryID string) error {
	q := make(url.Values, 1)
	q.Set("tenant_id", strings.TrimSpace(tenantID))
	path := fmt.Sprintf("/v1/memory/%s", url.PathEscape(strings.TrimSpace(memoryID)))
	return c.doJSON(ctx, http.MethodDelete, path, q, nil, nil)
}
