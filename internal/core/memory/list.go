package memory

import (
	"context"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

// List returns the most recent memories for a tenant.
func (s *Service) List(ctx context.Context, tenantID string, limit int) ([]domain.Memory, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, domain.ErrInvalidInput
	}
	if err := s.ensureTenantExists(ctx, tenantID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	return s.repo.Search(ctx, tenantID, "", limit)
}
