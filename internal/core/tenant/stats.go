package tenant

import (
	"context"

	"github.com/pali-mem/pali/internal/domain"
)

type Stats struct {
	MemoryCount int64 `json:"memory_count"`
}

type TenantWithStats struct {
	Tenant Tenant
	Stats  Stats
}

type Tenant = domain.Tenant

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

func (s *Service) ListWithStats(ctx context.Context, limit int) ([]TenantWithStats, error) {
	tenants, err := s.repo.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	if len(tenants) == 0 {
		return []TenantWithStats{}, nil
	}

	countsRepo, ok := s.repo.(domain.TenantMemoryCountsRepository)
	if !ok || countsRepo == nil {
		out := make([]TenantWithStats, 0, len(tenants))
		for _, tenant := range tenants {
			stats, err := s.Stats(ctx, tenant.ID)
			if err != nil {
				return nil, err
			}
			out = append(out, TenantWithStats{Tenant: tenant, Stats: stats})
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

	out := make([]TenantWithStats, 0, len(tenants))
	for _, tenant := range tenants {
		out = append(out, TenantWithStats{
			Tenant: tenant,
			Stats: Stats{
				MemoryCount: counts[tenant.ID],
			},
		})
	}
	return out, nil
}
