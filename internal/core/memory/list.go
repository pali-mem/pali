package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/pali-mem/pali/internal/domain"
)

const (
	defaultListPageLimit = 50
	maxListPageLimit     = 200
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

// ListPageOptions selects one page of a tenant's memories.
type ListPageOptions struct {
	Limit  int
	Offset int
	Tiers  []domain.MemoryTier
	Kinds  []domain.MemoryKind
}

// ListPageResult is one page of memories, newest first. NextOffset is nil when
// there are no further pages.
type ListPageResult struct {
	Items      []domain.Memory
	NextOffset *int
}

// ListPage returns one page of a tenant's memories ordered by creation time,
// newest first. Limit defaults to 50 and is capped at 200.
func (s *Service) ListPage(ctx context.Context, tenantID string, opts ListPageOptions) (ListPageResult, error) {
	if strings.TrimSpace(tenantID) == "" || opts.Offset < 0 {
		return ListPageResult{}, domain.ErrInvalidInput
	}
	if err := s.ensureTenantExists(ctx, tenantID); err != nil {
		return ListPageResult{}, err
	}
	repo, ok := s.repo.(domain.MemoryListRepository)
	if !ok || repo == nil {
		return ListPageResult{}, fmt.Errorf("memory repository does not support paged listing")
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = defaultListPageLimit
	}
	if limit > maxListPageLimit {
		limit = maxListPageLimit
	}

	// Fetch one extra row to learn whether another page exists.
	items, err := repo.ListPage(ctx, tenantID, domain.MemoryListFilter{
		Limit:  limit + 1,
		Offset: opts.Offset,
		Tiers:  opts.Tiers,
		Kinds:  opts.Kinds,
	})
	if err != nil {
		return ListPageResult{}, err
	}
	result := ListPageResult{Items: items}
	if len(items) > limit {
		result.Items = items[:limit]
		next := opts.Offset + limit
		result.NextOffset = &next
	}
	return result, nil
}
