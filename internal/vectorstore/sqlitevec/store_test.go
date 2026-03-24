package sqlitevec

import (
	"context"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

func TestStoreUpsertSearchDelete(t *testing.T) {
	ctx := context.Background()
	db, err := sqliterepo.Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant 1"})
	require.NoError(t, err)
	_, err = memoryRepo.Store(ctx, domain.Memory{ID: "m1", TenantID: "tenant_1", Content: "m1", Tier: domain.MemoryTierSemantic})
	require.NoError(t, err)
	_, err = memoryRepo.Store(ctx, domain.Memory{ID: "m2", TenantID: "tenant_1", Content: "m2", Tier: domain.MemoryTierSemantic})
	require.NoError(t, err)

	store := NewStore(db)
	require.NoError(t, store.Upsert(ctx, "tenant_1", "m1", []float32{1, 0}))
	require.NoError(t, store.Upsert(ctx, "tenant_1", "m2", []float32{0, 1}))

	candidates, err := store.Search(ctx, "tenant_1", []float32{0.9, 0.1}, 2)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "m1", candidates[0].MemoryID)

	require.NoError(t, store.Delete(ctx, "tenant_1", "m1"))
	candidates, err = store.Search(ctx, "tenant_1", []float32{0.9, 0.1}, 2)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "m2", candidates[0].MemoryID)
}

func TestStoreUpsertBatch(t *testing.T) {
	ctx := context.Background()
	db, err := sqliterepo.Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant 1"})
	require.NoError(t, err)
	_, err = memoryRepo.Store(ctx, domain.Memory{ID: "m1", TenantID: "tenant_1", Content: "m1", Tier: domain.MemoryTierSemantic})
	require.NoError(t, err)
	_, err = memoryRepo.Store(ctx, domain.Memory{ID: "m2", TenantID: "tenant_1", Content: "m2", Tier: domain.MemoryTierSemantic})
	require.NoError(t, err)

	store := NewStore(db)
	require.NoError(t, store.UpsertBatch(ctx, []domain.VectorUpsert{
		{TenantID: "tenant_1", MemoryID: "m1", Embedding: []float32{1, 0}},
		{TenantID: "tenant_1", MemoryID: "m2", Embedding: []float32{0, 1}},
	}))

	candidates, err := store.Search(ctx, "tenant_1", []float32{0.9, 0.1}, 2)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	require.Equal(t, "m1", candidates[0].MemoryID)
}
