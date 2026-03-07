package memory

import (
	"context"
	"strings"

	"github.com/vein05/pali/internal/domain"
)

func (s *Service) markIndexState(
	ctx context.Context,
	tenantID string,
	memoryIDs []string,
	op domain.MemoryIndexOperation,
	state domain.MemoryIndexState,
	err error,
) {
	if s == nil {
		return
	}
	repo, ok := s.repo.(domain.MemoryIndexStateRepository)
	if !ok || repo == nil {
		return
	}
	lastError := ""
	if err != nil {
		lastError = strings.TrimSpace(err.Error())
	}
	if markErr := repo.MarkIndexState(ctx, tenantID, memoryIDs, op, state, lastError); markErr != nil {
		s.logDebugf(
			"[pali-index] tenant=%s op=%s state=%s mark_error=%v",
			tenantID,
			op,
			state,
			markErr,
		)
	}
}
