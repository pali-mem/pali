package sqlite

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/domain"
	"github.com/vein05/pali/test/testutil"
)

func TestTenantRepositoryCreateExists(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

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
