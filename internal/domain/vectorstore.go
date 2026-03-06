package domain

import "context"

type VectorstoreCandidate struct {
	MemoryID   string
	Similarity float64
}

type VectorUpsert struct {
	TenantID  string
	MemoryID  string
	Embedding []float32
}

type VectorStore interface {
	Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error
	Delete(ctx context.Context, tenantID, memoryID string) error
	Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]VectorstoreCandidate, error)
}

// VectorBatchStore is an optional extension for stores that can upsert
// multiple embeddings in one operation.
type VectorBatchStore interface {
	UpsertBatch(ctx context.Context, upserts []VectorUpsert) error
}
