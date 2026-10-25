package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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

// GetMemory calls GET /v1/memory/:id?tenant_id=...
func (c *Client) GetMemory(ctx context.Context, tenantID, memoryID string) (MemoryResponse, error) {
	var out MemoryResponse
	q := make(url.Values, 1)
	q.Set("tenant_id", strings.TrimSpace(tenantID))
	path := fmt.Sprintf("/v1/memory/%s", url.PathEscape(strings.TrimSpace(memoryID)))
	err := c.doJSON(ctx, http.MethodGet, path, q, nil, &out)
	return out, err
}

// ListMemory calls GET /v1/memory. Pass the returned NextOffset as Offset to fetch the next page.
func (c *Client) ListMemory(ctx context.Context, req ListMemoryRequest) (ListMemoryResponse, error) {
	var out ListMemoryResponse
	q := make(url.Values, 5)
	q.Set("tenant_id", strings.TrimSpace(req.TenantID))
	if req.Limit > 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Offset > 0 {
		q.Set("offset", strconv.Itoa(req.Offset))
	}
	if csv := joinCSV(req.Tiers); csv != "" {
		q.Set("tier", csv)
	}
	if csv := joinCSV(req.Kinds); csv != "" {
		q.Set("kind", csv)
	}
	err := c.doJSON(ctx, http.MethodGet, "/v1/memory", q, nil, &out)
	return out, err
}

// UpdateMemory calls PATCH /v1/memory/:id?tenant_id=... and returns the updated memory.
func (c *Client) UpdateMemory(ctx context.Context, tenantID, memoryID string, req UpdateMemoryRequest) (MemoryResponse, error) {
	var out MemoryResponse
	q := make(url.Values, 1)
	q.Set("tenant_id", strings.TrimSpace(tenantID))
	path := fmt.Sprintf("/v1/memory/%s", url.PathEscape(strings.TrimSpace(memoryID)))
	err := c.doJSON(ctx, http.MethodPatch, path, q, req, &out)
	return out, err
}

// DeleteMemory calls DELETE /v1/memory/:id?tenant_id=...
func (c *Client) DeleteMemory(ctx context.Context, tenantID, memoryID string) error {
	q := make(url.Values, 1)
	q.Set("tenant_id", strings.TrimSpace(tenantID))
	path := fmt.Sprintf("/v1/memory/%s", url.PathEscape(strings.TrimSpace(memoryID)))
	return c.doJSON(ctx, http.MethodDelete, path, q, nil, nil)
}
