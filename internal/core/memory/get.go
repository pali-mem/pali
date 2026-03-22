package memory

import (
	"context"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

func (s *Service) Get(ctx context.Context, tenantID, memoryID string) (*domain.Memory, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" {
		return nil, domain.ErrInvalidInput
	}
	if err := s.ensureTenantExists(ctx, tenantID); err != nil {
		return nil, err
	}

	items, err := s.repo.GetByIDs(ctx, tenantID, []string{memoryID})
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return &items[0], nil
}
