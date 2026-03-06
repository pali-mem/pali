package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/domain"
	"github.com/vein05/pali/test/testutil"
)

func TestMemoryRepositoryStoreSearchDelete(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

	tenantRepo := NewTenantRepository(db)
	memRepo := NewMemoryRepository(db)

	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant One"})
	require.NoError(t, err)

	stored, err := memRepo.Store(ctx, domain.Memory{
		TenantID:   "tenant_1",
		Content:    "User prefers Go for backend systems",
		Tier:       domain.MemoryTierSemantic,
		Tags:       []string{"preferences", "golang"},
		Source:     "seed",
		CreatedBy:  domain.MemoryCreatedByUser,
		Kind:       domain.MemoryKindObservation,
		Importance: 0.77,
	})
	require.NoError(t, err)
	require.NotEmpty(t, stored.ID)

	results, err := memRepo.Search(ctx, "tenant_1", "Go", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, stored.ID, results[0].ID)
	require.InDelta(t, 0.77, results[0].Importance, 0.0001)
	require.Equal(t, "seed", results[0].Source)
	require.Equal(t, domain.MemoryCreatedByUser, results[0].CreatedBy)
	require.Equal(t, domain.MemoryKindObservation, results[0].Kind)
	require.Equal(t, 0, results[0].RecallCount)

	byID, err := memRepo.GetByIDs(ctx, "tenant_1", []string{stored.ID})
	require.NoError(t, err)
	require.Len(t, byID, 1)
	require.Equal(t, stored.ID, byID[0].ID)
	require.Equal(t, "seed", byID[0].Source)
	require.Equal(t, domain.MemoryCreatedByUser, byID[0].CreatedBy)
	require.Equal(t, domain.MemoryKindObservation, byID[0].Kind)
	require.Equal(t, 0, byID[0].RecallCount)

	before := byID[0].LastAccessedAt
	beforeRecalled := byID[0].LastRecalledAt
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, memRepo.Touch(ctx, "tenant_1", []string{stored.ID}))
	afterTouch, err := memRepo.GetByIDs(ctx, "tenant_1", []string{stored.ID})
	require.NoError(t, err)
	require.Len(t, afterTouch, 1)
	require.True(t, afterTouch[0].LastAccessedAt.After(before) || afterTouch[0].LastAccessedAt.Equal(before))
	require.True(t, afterTouch[0].LastRecalledAt.After(beforeRecalled) || afterTouch[0].LastRecalledAt.Equal(beforeRecalled))
	require.Equal(t, 1, afterTouch[0].RecallCount)

	require.NoError(t, memRepo.Delete(ctx, "tenant_1", stored.ID))

	results, err = memRepo.Search(ctx, "tenant_1", "", 10)
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestMemoryRepositoryStoreBatch(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

	tenantRepo := NewTenantRepository(db)
	memRepo := NewMemoryRepository(db)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant One"})
	require.NoError(t, err)

	stored, err := memRepo.StoreBatch(ctx, []domain.Memory{
		{
			TenantID:   "tenant_1",
			Content:    "User prefers tea.",
			Tier:       domain.MemoryTierSemantic,
			Tags:       []string{"preference"},
			CreatedBy:  domain.MemoryCreatedByUser,
			Kind:       domain.MemoryKindObservation,
			Importance: 0.4,
		},
		{
			TenantID:   "tenant_1",
			Content:    "User moved to Austin in 2024.",
			Tier:       domain.MemoryTierSemantic,
			Tags:       []string{"profile"},
			CreatedBy:  domain.MemoryCreatedBySystem,
			Kind:       domain.MemoryKindEvent,
			Importance: 0.7,
		},
	})
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.NotEmpty(t, stored[0].ID)
	require.NotEmpty(t, stored[1].ID)

	results, err := memRepo.Search(ctx, "tenant_1", "", 10)
	require.NoError(t, err)
	require.Len(t, results, 2)

	byID, err := memRepo.GetByIDs(ctx, "tenant_1", []string{stored[0].ID, stored[1].ID})
	require.NoError(t, err)
	require.Len(t, byID, 2)
	require.Equal(t, stored[0].ID, byID[0].ID)
	require.Equal(t, stored[1].ID, byID[1].ID)
}

func TestMemoryRepositoryStoreBatchRollsBackOnError(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

	tenantRepo := NewTenantRepository(db)
	memRepo := NewMemoryRepository(db)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant One"})
	require.NoError(t, err)

	_, err = memRepo.StoreBatch(ctx, []domain.Memory{
		{
			TenantID: "tenant_1",
			Content:  "valid memory",
		},
		{
			TenantID: "tenant_1",
			Content:  "",
		},
	})
	require.Error(t, err)

	results, err := memRepo.Search(ctx, "tenant_1", "", 10)
	require.NoError(t, err)
	require.Empty(t, results)
}
