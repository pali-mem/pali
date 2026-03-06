package tenant

import (
	"context"
	"strings"

	"github.com/vein05/pali/internal/domain"
)

func (s *Service) Exists(ctx context.Context, tenantID string) (bool, error) {
	if strings.TrimSpace(tenantID) == "" {
		return false, domain.ErrInvalidInput
	}
	return s.repo.Exists(ctx, tenantID)
}
