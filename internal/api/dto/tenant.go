// Package dto defines request and response payloads for tenant endpoints.
package dto

import "time"

// CreateTenantRequest describes a tenant creation request.
type CreateTenantRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateTenantResponse reports a newly created tenant.
type CreateTenantResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// TenantItemResponse is one tenant with its memory count.
type TenantItemResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	MemoryCount int64     `json:"memory_count"`
}

// ListTenantsResponse wraps the tenants returned by GET /v1/tenants.
type ListTenantsResponse struct {
	Items []TenantItemResponse `json:"items"`
}

// TenantStatsResponse reports aggregate tenant statistics.
type TenantStatsResponse struct {
	TenantID       string           `json:"tenant_id"`
	MemoryCount    int64            `json:"memory_count"`
	ByTier         map[string]int64 `json:"by_tier"`
	ByKind         map[string]int64 `json:"by_kind"`
	TotalRecalls   int64            `json:"total_recalls"`
	NewestMemoryAt *time.Time       `json:"newest_memory_at"`
}
