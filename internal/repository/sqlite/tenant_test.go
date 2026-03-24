package sqlite

import (
	"context"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

func TestTenantRepositoryCreateExists(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer closeDB(db)

	repo := NewTenantRepository(db)
	created, err := repo.Create(ctx, domain.Tenant{
		ID:   "tenant_1",
		Name: "Tenant One",
	})
	require.NoError(t, err)
	require.Equal(t, "tenant_1", created.ID)

	exists, err := repo.Exists(ctx, "tenant_1")
	require.NoError(t, err)
	require.True(t, exists)

	exists, err = repo.Exists(ctx, "tenant_missing")
	require.NoError(t, err)
	require.False(t, exists)

	list, err := repo.List(ctx, 10)
	require.NoError(t, err)
	require.NotEmpty(t, list)
	require.Equal(t, "tenant_1", list[0].ID)
}

func TestTenantRepositoryListMemoryCounts(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer closeDB(db)

	tenantRepo := NewTenantRepository(db)
	memoryRepo := NewMemoryRepository(db)

	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant One"})
	require.NoError(t, err)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_2", Name: "Tenant Two"})
	require.NoError(t, err)

	_, err = memoryRepo.Store(ctx, domain.Memory{TenantID: "tenant_1", Content: "one", Tier: domain.MemoryTierSemantic})
	require.NoError(t, err)
	_, err = memoryRepo.Store(ctx, domain.Memory{TenantID: "tenant_1", Content: "two", Tier: domain.MemoryTierSemantic})
	require.NoError(t, err)

	counts, err := tenantRepo.ListMemoryCounts(ctx, []string{"tenant_1", "tenant_2"})
	require.NoError(t, err)
	require.Equal(t, int64(2), counts["tenant_1"])
	require.Equal(t, int64(0), counts["tenant_2"])
}
