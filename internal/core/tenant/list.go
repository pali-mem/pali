package tenant

import (
	"context"

	"github.com/pali-mem/pali/internal/domain"
)

// List returns tenant records up to the requested limit.
func (s *Service) List(ctx context.Context, limit int) ([]domain.Tenant, error) {
	return s.repo.List(ctx, limit)
}
