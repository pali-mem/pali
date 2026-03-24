// Package mock provides a minimal vector-store implementation for tests.
package mock

import (
	"context"

	"github.com/pali-mem/pali/internal/domain"
)

type store struct{}

// NewStore returns a no-op vector store used in tests.
func NewStore() *store { return &store{} }

func (s *store) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	_ = ctx
	_ = tenantID
	_ = memoryID
	_ = embedding
	return nil
}

func (s *store) Delete(ctx context.Context, tenantID, memoryID string) error {
	_ = ctx
	_ = tenantID
	_ = memoryID
	return nil
}

func (s *store) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	_ = ctx
	_ = tenantID
	_ = embedding
	_ = topK
	return []domain.VectorstoreCandidate{}, nil
}
