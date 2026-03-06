package tenant

import "github.com/vein05/pali/internal/domain"

type Service struct {
	repo domain.TenantRepository
}

func NewService(repo domain.TenantRepository) *Service {
	return &Service{repo: repo}
}
