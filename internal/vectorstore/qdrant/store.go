package qdrant

import (
	"context"
	"fmt"

	"github.com/pali-mem/pali/internal/domain"
)

type Store struct {
	client *Client
}

func NewStore(client *Client) *Store { return &Store{client: client} }

func (s *Store) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("qdrant store client is nil")
	}
	return s.client.Upsert(ctx, tenantID, memoryID, embedding)
}

func (s *Store) UpsertBatch(ctx context.Context, upserts []domain.VectorUpsert) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("qdrant store client is nil")
	}
	return s.client.UpsertBatch(ctx, upserts)
}

func (s *Store) Delete(ctx context.Context, tenantID, memoryID string) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("qdrant store client is nil")
	}
	return s.client.Delete(ctx, tenantID, memoryID)
}

func (s *Store) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("qdrant store client is nil")
	}
	return s.client.Search(ctx, tenantID, embedding, topK)
}
