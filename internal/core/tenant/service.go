package tenant

import "github.com/pali-mem/pali/internal/domain"

type Service struct {
	repo domain.TenantRepository
}

func NewService(repo domain.TenantRepository) *Service {
	return &Service{repo: repo}
}
