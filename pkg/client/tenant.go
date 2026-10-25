package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// CreateTenant calls POST /v1/tenants.
func (c *Client) CreateTenant(ctx context.Context, req CreateTenantRequest) (CreateTenantResponse, error) {
	var out CreateTenantResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/tenants", nil, req, &out)
	return out, err
}

// ListTenants calls GET /v1/tenants. A limit of 0 uses the server default (100, max 500).
// Tenant-scoped tokens only see their own tenant.
func (c *Client) ListTenants(ctx context.Context, limit int) (ListTenantsResponse, error) {
	var out ListTenantsResponse
	var q url.Values
	if limit > 0 {
		q = url.Values{"limit": []string{strconv.Itoa(limit)}}
	}
	err := c.doJSON(ctx, http.MethodGet, "/v1/tenants", q, nil, &out)
	return out, err
}

// TenantStats calls GET /v1/tenants/:id/stats.
func (c *Client) TenantStats(ctx context.Context, tenantID string) (TenantStatsResponse, error) {
	var out TenantStatsResponse
	path := fmt.Sprintf("/v1/tenants/%s/stats", url.PathEscape(strings.TrimSpace(tenantID)))
	err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out)
	return out, err
}
