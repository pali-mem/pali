package dto

import "time"

type CreateTenantRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type CreateTenantResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type TenantStatsResponse struct {
	TenantID    string `json:"tenant_id"`
	MemoryCount int64  `json:"memory_count"`
}
