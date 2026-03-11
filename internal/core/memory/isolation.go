package memory

import (
	"context"

	"github.com/pali-mem/pali/internal/domain"
)

func (s *Service) ensureTenantExists(ctx context.Context, tenantID string) error {
	exists, err := s.tenantRepo.Exists(ctx, tenantID)
	if err != nil {
		return err
	}
	if !exists {
		return domain.ErrNotFound
	}
	return nil
}
