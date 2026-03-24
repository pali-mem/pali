package domain

import "context"

// VectorstoreCandidate is a search result from the vector store.
type VectorstoreCandidate struct {
	MemoryID   string
	Similarity float64
}

// VectorUpsert describes a vector to persist.
type VectorUpsert struct {
	TenantID  string
	MemoryID  string
	Embedding []float32
}

// VectorStore persists and retrieves embeddings.
type VectorStore interface {
	Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error
	Delete(ctx context.Context, tenantID, memoryID string) error
	Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]VectorstoreCandidate, error)
}

// VectorBatchStore is an optional extension for stores that can upsert
// multiple embeddings in one operation.
// VectorBatchStore upserts multiple embeddings at once.
type VectorBatchStore interface {
	UpsertBatch(ctx context.Context, upserts []VectorUpsert) error
}
