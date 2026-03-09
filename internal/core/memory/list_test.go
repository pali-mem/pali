package memory

import (
	"context"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

type listRepoStub struct {
	searchCalled bool
	searchQuery  string
	searchTopK   int
	result       []domain.Memory
}

func (r *listRepoStub) Store(ctx context.Context, memory domain.Memory) (domain.Memory, error) {
	return memory, nil
}
func (r *listRepoStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	return nil
}
func (r *listRepoStub) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	r.searchCalled = true
	r.searchQuery = query
	r.searchTopK = topK
	return r.result, nil
}
func (r *listRepoStub) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}
func (r *listRepoStub) Touch(ctx context.Context, tenantID string, ids []string) error {
	return nil
}

func TestList_InvalidInput(t *testing.T) {
	svc := NewService(
		&listRepoStub{},
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vectorStoreStub{},
		embedderStub{},
		scorerStub{},
	)

	_, err := svc.List(context.Background(), "", 10)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestList_DefaultLimit(t *testing.T) {
	repo := &listRepoStub{
		result: []domain.Memory{
			{ID: "mem_1", TenantID: "tenant_1"},
		},
	}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vectorStoreStub{},
		embedderStub{},
		scorerStub{},
	)

	out, err := svc.List(context.Background(), "tenant_1", 0)
	require.NoError(t, err)
	require.True(t, repo.searchCalled)
	require.Equal(t, "", repo.searchQuery)
	require.Equal(t, 50, repo.searchTopK)
	require.Len(t, out, 1)
}

func TestList_RespectsExplicitLimit(t *testing.T) {
	repo := &listRepoStub{
		result: []domain.Memory{
			{ID: "mem_1", TenantID: "tenant_1"},
		},
	}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vectorStoreStub{},
		embedderStub{},
		scorerStub{},
	)

	_, err := svc.List(context.Background(), "tenant_1", 5)
	require.NoError(t, err)
	require.Equal(t, 5, repo.searchTopK)
}
