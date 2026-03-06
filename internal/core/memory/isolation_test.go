package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/domain"
)

type memoryRepoStub struct{}

func (memoryRepoStub) Store(ctx context.Context, m domain.Memory) (domain.Memory, error) {
	return m, nil
}
func (memoryRepoStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	return nil
}
func (memoryRepoStub) Search(ctx context.Context, tenantID, query string, topK int) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}
func (memoryRepoStub) GetByIDs(ctx context.Context, tenantID string, ids []string) ([]domain.Memory, error) {
	return []domain.Memory{}, nil
}
func (memoryRepoStub) Touch(ctx context.Context, tenantID string, ids []string) error {
	return nil
}

type tenantRepoStub struct {
	existsByID      map[string]bool
	memoryCountByID map[string]int64
}

func (t tenantRepoStub) Create(ctx context.Context, tenant domain.Tenant) (domain.Tenant, error) {
	return tenant, nil
}
func (t tenantRepoStub) Exists(ctx context.Context, tenantID string) (bool, error) {
	return t.existsByID[tenantID], nil
}
func (t tenantRepoStub) MemoryCount(ctx context.Context, tenantID string) (int64, error) {
	if t.memoryCountByID != nil {
		if count, ok := t.memoryCountByID[tenantID]; ok {
			return count, nil
		}
	}
	return 0, nil
}
func (t tenantRepoStub) List(ctx context.Context, limit int) ([]domain.Tenant, error) {
	return []domain.Tenant{}, nil
}

type vectorStoreStub struct{}

func (vectorStoreStub) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	return nil
}
func (vectorStoreStub) Delete(ctx context.Context, tenantID, memoryID string) error {
	return nil
}
func (vectorStoreStub) Search(ctx context.Context, tenantID string, embedding []float32, topK int) ([]domain.VectorstoreCandidate, error) {
	return []domain.VectorstoreCandidate{}, nil
}

type embedderStub struct{}

func (embedderStub) Embed(ctx context.Context, text string) ([]float32, error) {
	return []float32{1}, nil
}

type scorerStub struct{}

func (scorerStub) Score(ctx context.Context, text string) (float64, error) {
	return 0.5, nil
}

func TestServiceStore_UnknownTenant(t *testing.T) {
	svc := NewService(memoryRepoStub{}, tenantRepoStub{existsByID: map[string]bool{}}, vectorStoreStub{}, embedderStub{}, scorerStub{})
	_, err := svc.Store(context.Background(), StoreInput{
		TenantID: "tenant_missing",
		Content:  "x",
	})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestServiceSearch_UnknownTenant(t *testing.T) {
	svc := NewService(memoryRepoStub{}, tenantRepoStub{existsByID: map[string]bool{}}, vectorStoreStub{}, embedderStub{}, scorerStub{})
	_, err := svc.Search(context.Background(), "tenant_missing", "q", 10)
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestServiceDelete_UnknownTenant(t *testing.T) {
	svc := NewService(memoryRepoStub{}, tenantRepoStub{existsByID: map[string]bool{}}, vectorStoreStub{}, embedderStub{}, scorerStub{})
	err := svc.Delete(context.Background(), "tenant_missing", "mem_1")
	require.ErrorIs(t, err, domain.ErrNotFound)
}
