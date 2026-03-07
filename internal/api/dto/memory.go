package dto

import "time"

type StoreMemoryRequest struct {
	TenantID  string   `json:"tenant_id"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	Tier      string   `json:"tier"`
	Kind      string   `json:"kind,omitempty"`
	Source    string   `json:"source"`
	CreatedBy string   `json:"created_by"`
}

type StoreMemoryBatchRequest struct {
	Items []StoreMemoryRequest `json:"items"`
}

type SearchMemoryRequest struct {
	TenantID     string   `json:"tenant_id"`
	Query        string   `json:"query"`
	TopK         int      `json:"top_k"`
	MinScore     float64  `json:"min_score"`
	Tiers        []string `json:"tiers"`
	Kinds        []string `json:"kinds,omitempty"`
	DisableTouch bool     `json:"disable_touch,omitempty"`
	Debug        bool     `json:"debug,omitempty"`
}

type StoreMemoryResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type StoreMemoryBatchResponse struct {
	Items []StoreMemoryResponse `json:"items"`
}

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

type SearchMemoryResponse struct {
	Items []MemoryResponse      `json:"items"`
	Debug *SearchMemoryDebugDTO `json:"debug,omitempty"`
}

type SearchMemoryDebugDTO struct {
	Plan    SearchPlanDebugDTO      `json:"plan"`
	Ranking []SearchRankingDebugDTO `json:"ranking,omitempty"`
}

type SearchPlanDebugDTO struct {
	Intent           string   `json:"intent"`
	Confidence       float64  `json:"confidence"`
	Entities         []string `json:"entities,omitempty"`
	Relations        []string `json:"relations,omitempty"`
	TimeConstraints  []string `json:"time_constraints,omitempty"`
	RequiredEvidence string   `json:"required_evidence,omitempty"`
	FallbackPath     []string `json:"fallback_path,omitempty"`
}

type SearchRankingDebugDTO struct {
	Rank         int     `json:"rank"`
	MemoryID     string  `json:"memory_id"`
	Kind         string  `json:"kind"`
	Tier         string  `json:"tier"`
	LexicalScore float64 `json:"lexical_score"`
	QueryOverlap float64 `json:"query_overlap"`
	RouteFit     float64 `json:"route_fit"`
}
