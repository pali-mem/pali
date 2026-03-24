package tenant

import (
	"context"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

// Create stores a new tenant record.
func (s *Service) Create(ctx context.Context, t domain.Tenant) (domain.Tenant, error) {
	if strings.TrimSpace(t.ID) == "" || strings.TrimSpace(t.Name) == "" {
		return domain.Tenant{}, domain.ErrInvalidInput
	}
	return s.repo.Create(ctx, t)
}
