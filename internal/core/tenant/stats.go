// Package tenant provides tenant service helpers and statistics views.
package tenant

import (
	"context"

	"github.com/pali-mem/pali/internal/domain"
)

// Stats summarizes tenant-level counters.
type Stats struct {
	MemoryCount int64 `json:"memory_count"`
}

// WithStats couples a tenant record with its stats.
type WithStats struct {
	Tenant Tenant
	Stats  Stats
}

// Tenant is the tenant domain model re-exported for convenience.
type Tenant = domain.Tenant

// Stats returns aggregate statistics for a tenant.
func (s *Service) Stats(ctx context.Context, tenantID string) (Stats, error) {
	exists, err := s.repo.Exists(ctx, tenantID)
	if err != nil {
		return Stats{}, err
	}
	if !exists {
		return Stats{}, domain.ErrNotFound
	}

	count, err := s.repo.MemoryCount(ctx, tenantID)
	if err != nil {
		return Stats{}, err
	}

	return Stats{MemoryCount: count}, nil
}

// ListWithStats returns tenants together with their aggregate statistics.
func (s *Service) ListWithStats(ctx context.Context, limit int) ([]WithStats, error) {
	tenants, err := s.repo.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	if len(tenants) == 0 {
		return []WithStats{}, nil
	}

	countsRepo, ok := s.repo.(domain.TenantMemoryCountsRepository)
	if !ok || countsRepo == nil {
		out := make([]WithStats, 0, len(tenants))
		for _, tenant := range tenants {
			stats, err := s.Stats(ctx, tenant.ID)
			if err != nil {
				return nil, err
			}
			out = append(out, WithStats{Tenant: tenant, Stats: stats})
		}
		return out, nil
	}

	tenantIDs := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		tenantIDs = append(tenantIDs, tenant.ID)
	}
	counts, err := countsRepo.ListMemoryCounts(ctx, tenantIDs)
	if err != nil {
		return nil, err
	}

	out := make([]WithStats, 0, len(tenants))
	for _, tenant := range tenants {
		out = append(out, WithStats{
			Tenant: tenant,
			Stats: Stats{
				MemoryCount: counts[tenant.ID],
			},
		})
	}
	return out, nil
}
