package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

// Delete removes a memory, its vector embedding, and its entity facts.
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
	if err := s.deleteEntityFactsForMemories(ctx, tenantID, []string{memoryID}); err != nil {
		return fmt.Errorf("memory deleted but entity fact cleanup failed: %w", err)
	}
	return nil
}

// deleteEntityFactsForMemories removes entity facts sourced from the given
// memories. SQLite also cascades on memory delete; Neo4j does not.
func (s *Service) deleteEntityFactsForMemories(ctx context.Context, tenantID string, memoryIDs []string) error {
	repo, ok := s.entityRepo.(domain.EntityFactMemoryDeleteRepository)
	if !ok || repo == nil {
		return nil
	}
	return repo.DeleteByMemoryIDs(ctx, tenantID, memoryIDs)
}
