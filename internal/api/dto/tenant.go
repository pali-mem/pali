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

// TenantStatsResponse reports aggregate tenant statistics.
type TenantStatsResponse struct {
	TenantID    string `json:"tenant_id"`
	MemoryCount int64  `json:"memory_count"`
}
