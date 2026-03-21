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
	Debug         bool     `json:"debug,omitempty"`
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

// SearchPlanDebug is planner diagnostics returned when debug mode is enabled.
type SearchPlanDebug struct {
	Intent           string   `json:"intent"`
	Confidence       float64  `json:"confidence"`
	AnswerType       string   `json:"answer_type,omitempty"`
	Entities         []string `json:"entities,omitempty"`
	Relations        []string `json:"relations,omitempty"`
	TimeConstraints  []string `json:"time_constraints,omitempty"`
	RequiredEvidence string   `json:"required_evidence,omitempty"`
	FallbackPath     []string `json:"fallback_path,omitempty"`
}

// SearchRankingDebug is ranking diagnostics for one memory candidate.
type SearchRankingDebug struct {
	Rank         int     `json:"rank"`
	MemoryID     string  `json:"memory_id"`
	Kind         string  `json:"kind"`
	Tier         string  `json:"tier"`
	LexicalScore float64 `json:"lexical_score"`
	QueryOverlap float64 `json:"query_overlap"`
	RouteFit     float64 `json:"route_fit"`
}

// SearchMemoryDebug is debug metadata for search operations.
type SearchMemoryDebug struct {
	Plan    SearchPlanDebug      `json:"plan"`
	Ranking []SearchRankingDebug `json:"ranking,omitempty"`
}

// SearchMemoryResponse is returned by POST /v1/memory/search.
type SearchMemoryResponse struct {
	Items []MemoryResponse   `json:"items"`
	Debug *SearchMemoryDebug `json:"debug,omitempty"`
}

// IngestMemoryResponse is returned by async ingest endpoints.
type IngestMemoryResponse struct {
	IngestID   string    `json:"ingest_id"`
	MemoryIDs  []string  `json:"memory_ids"`
	JobIDs     []string  `json:"job_ids"`
	AcceptedAt time.Time `json:"accepted_at"`
}

// PostprocessJobResponse is a single job item returned by job endpoints.
type PostprocessJobResponse struct {
	ID          string    `json:"id"`
	IngestID    string    `json:"ingest_id"`
	TenantID    string    `json:"tenant_id"`
	MemoryID    string    `json:"memory_id"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	MaxAttempts int       `json:"max_attempts"`
	AvailableAt time.Time `json:"available_at"`
	LeaseOwner  string    `json:"lease_owner,omitempty"`
	LeasedUntil time.Time `json:"leased_until,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ListPostprocessJobsRequest is the query payload for GET /v1/memory/jobs.
type ListPostprocessJobsRequest struct {
	TenantID string
	Statuses []string
	Types    []string
	Limit    int
}

// ListPostprocessJobsResponse is returned by GET /v1/memory/jobs.
type ListPostprocessJobsResponse struct {
	Items []PostprocessJobResponse `json:"items"`
}
