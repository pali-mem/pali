package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/vein05/pali/internal/domain"
)

func (s *Service) Delete(ctx context.Context, tenantID, memoryID string) error {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(memoryID) == "" {
		return domain.ErrInvalidInput
	}
	if err := s.ensureTenantExists(ctx, tenantID); err != nil {
		return err
	}
	if s.vector == nil {
		return fmt.Errorf("memory service vector store is not initialized")
	}

	if err := s.repo.Delete(ctx, tenantID, memoryID); err != nil {
		return err
	}
	if err := s.vector.Delete(ctx, tenantID, memoryID); err != nil {
		return err
	}
	return nil
}
