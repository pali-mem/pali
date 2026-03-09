package memory

import (
	"context"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

type deleteRepoStub struct {
	deleteCalled bool
}

func (r *deleteRepoStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	r.deleteCalled = true
	return nil
}
func (r *deleteRepoStub) Store(ctx context.Context, memory domain.Memory) (domain.Memory, error) {
	return memory, nil
}
func (r *deleteRepoStub) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}
func (r *deleteRepoStub) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}
func (r *deleteRepoStub) Touch(ctx context.Context, tenantID string, ids []string) error {
	return nil
}

type deleteVectorStoreStub struct {
	deleteCalls int
	deleteErr   error
}

func (v *deleteVectorStoreStub) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	return nil
}
func (v *deleteVectorStoreStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	v.deleteCalls++
	return v.deleteErr
}
func (v *deleteVectorStoreStub) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	return []domain.VectorstoreCandidate{}, nil
}

func TestDelete_InvalidInput(t *testing.T) {
	svc := NewService(
		&deleteRepoStub{},
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		&deleteVectorStoreStub{},
		embedderStub{},
		scorerStub{},
	)

	require.ErrorIs(t, svc.Delete(context.Background(), "", "mem"), domain.ErrInvalidInput)
	require.ErrorIs(t, svc.Delete(context.Background(), "tenant_1", ""), domain.ErrInvalidInput)
}

func TestDelete_UnknownTenant(t *testing.T) {
	svc := NewService(
		&deleteRepoStub{},
		tenantRepoStub{existsByID: map[string]bool{}},
		&deleteVectorStoreStub{},
		embedderStub{},
		scorerStub{},
	)

	require.ErrorIs(t, svc.Delete(context.Background(), "tenant_missing", "mem_1"), domain.ErrNotFound)
}

func TestDelete_RequiresInitializedVectorStore(t *testing.T) {
	svc := NewService(
		&deleteRepoStub{},
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		nil,
		embedderStub{},
		scorerStub{},
	)

	require.ErrorContains(t, svc.Delete(context.Background(), "tenant_1", "mem_1"), "vector store is not initialized")
}

func TestDelete_DeletesFromRepoAndVector(t *testing.T) {
	repo := &deleteRepoStub{}
	vector := &deleteVectorStoreStub{}
	svc := NewService(
		repo,
		tenantRepoStub{existsByID: map[string]bool{"tenant_1": true}},
		vector,
		embedderStub{},
		scorerStub{},
	)

	err := svc.Delete(context.Background(), "tenant_1", "mem_1")
	require.NoError(t, err)
	require.True(t, repo.deleteCalled)
	require.Equal(t, 1, vector.deleteCalls)
}
