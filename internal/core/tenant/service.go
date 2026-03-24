// Package tenant provides tenant service helpers and statistics views.
package tenant

import "github.com/pali-mem/pali/internal/domain"

// Service coordinates tenant operations.
type Service struct {
	repo domain.TenantRepository
}

// NewService constructs a tenant service.
func NewService(repo domain.TenantRepository) *Service {
	return &Service{repo: repo}
}
