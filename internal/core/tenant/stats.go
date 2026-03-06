package tenant

import (
	"context"

	"github.com/vein05/pali/internal/domain"
)

type Stats struct {
	MemoryCount int64 `json:"memory_count"`
}

func (s *Service) Stats(ctx context.Context, tenantID string) (Stats, error) {
	exists, err := s.repo.Exists(ctx, tenantID)
	if err != nil {
		return Stats{}, err
	}
	if !exists {
		return Stats{}, domain.ErrNotFound
	}

	count, err := s.repo.MemoryCount(ctx, tenantID)
	if err != nil {
		return Stats{}, err
	}

	return Stats{MemoryCount: count}, nil
}
