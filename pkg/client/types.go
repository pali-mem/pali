package client

import "time"

// HealthResponse is returned by GET /health.
type HealthResponse struct {
	Status string `json:"status"`
	Time   string `json:"time"`
}

// CreateTenantRequest is the request payload for POST /v1/tenants.
type CreateTenantRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CreateTenantResponse is returned by POST /v1/tenants.
type CreateTenantResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// TenantStatsResponse is returned by GET /v1/tenants/:id/stats.
type TenantStatsResponse struct {
	TenantID    string `json:"tenant_id"`
	MemoryCount int64  `json:"memory_count"`
}

// StoreMemoryRequest is the request payload for POST /v1/memory.
type StoreMemoryRequest struct {
	TenantID  string   `json:"tenant_id"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	Tier      string   `json:"tier"`
	Kind      string   `json:"kind,omitempty"`
	Source    string   `json:"source,omitempty"`
	CreatedBy string   `json:"created_by,omitempty"`
}

// StoreMemoryResponse is returned by POST /v1/memory.
type StoreMemoryResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// StoreMemoryBatchRequest is the request payload for POST /v1/memory/batch.
type StoreMemoryBatchRequest struct {
	Items []StoreMemoryRequest `json:"items"`
}

// StoreMemoryBatchResponse is returned by POST /v1/memory/batch.
type StoreMemoryBatchResponse struct {
	Items []StoreMemoryResponse `json:"items"`
}

// SearchMemoryRequest is the request payload for POST /v1/memory/search.
type SearchMemoryRequest struct {
	TenantID      string   `json:"tenant_id"`
	Query         string   `json:"query"`
	TopK          int      `json:"top_k"`
	MinScore      float64  `json:"min_score,omitempty"`
	Tiers         []string `json:"tiers,omitempty"`
	Kinds         []string `json:"kinds,omitempty"`
	RetrievalKind string   `json:"retrieval_kind,omitempty"`
	DisableTouch  bool     `json:"disable_touch,omitempty"`
}

// MemoryResponse is a single memory item returned by search.
type MemoryResponse struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Content        string    `json:"content"`
	Tier           string    `json:"tier"`
	Tags           []string  `json:"tags"`
	Source         string    `json:"source"`
	CreatedBy      string    `json:"created_by"`
	Kind           string    `json:"kind"`
	RecallCount    int       `json:"recall_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
	LastRecalledAt time.Time `json:"last_recalled_at"`
}

// SearchMemoryResponse is returned by POST /v1/memory/search.
type SearchMemoryResponse struct {
	Items []MemoryResponse `json:"items"`
}
