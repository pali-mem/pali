package tenant

import (
	"context"

	"github.com/vein05/pali/internal/domain"
)

func (s *Service) List(ctx context.Context, limit int) ([]domain.Tenant, error) {
	return s.repo.List(ctx, limit)
}
