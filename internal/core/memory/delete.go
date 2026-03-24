package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

// Delete removes a memory and its vector embedding.
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

	s.markIndexState(
		ctx,
		tenantID,
		[]string{memoryID},
		domain.MemoryIndexOperationDelete,
		domain.MemoryIndexStatePending,
		nil,
	)
	if err := s.repo.Delete(ctx, tenantID, memoryID); err != nil {
		s.markIndexState(
			ctx,
			tenantID,
			[]string{memoryID},
			domain.MemoryIndexOperationDelete,
			domain.MemoryIndexStateFailed,
			err,
		)
		return err
	}
	if err := s.vector.Delete(ctx, tenantID, memoryID); err != nil {
		s.markIndexState(
			ctx,
			tenantID,
			[]string{memoryID},
			domain.MemoryIndexOperationDelete,
			domain.MemoryIndexStateFailed,
			err,
		)
		return err
	}
	s.markIndexState(
		ctx,
		tenantID,
		[]string{memoryID},
		domain.MemoryIndexOperationDelete,
		domain.MemoryIndexStateTombstoned,
		nil,
	)
	return nil
}
