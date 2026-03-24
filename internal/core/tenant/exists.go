package tenant

import (
	"context"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

// Exists reports whether a tenant exists.
func (s *Service) Exists(ctx context.Context, tenantID string) (bool, error) {
	if strings.TrimSpace(tenantID) == "" {
		return false, domain.ErrInvalidInput
	}
	return s.repo.Exists(ctx, tenantID)
}
