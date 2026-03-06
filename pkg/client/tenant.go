package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// CreateTenant calls POST /v1/tenants.
func (c *Client) CreateTenant(ctx context.Context, req CreateTenantRequest) (CreateTenantResponse, error) {
	var out CreateTenantResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/tenants", nil, req, &out)
	return out, err
}

// TenantStats calls GET /v1/tenants/:id/stats.
func (c *Client) TenantStats(ctx context.Context, tenantID string) (TenantStatsResponse, error) {
	var out TenantStatsResponse
	path := fmt.Sprintf("/v1/tenants/%s/stats", url.PathEscape(strings.TrimSpace(tenantID)))
	err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out)
	return out, err
}
