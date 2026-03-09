package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/pali-mem/pali/internal/domain"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

func TestEntityFactRepositoryStoreAndLookup(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

	tenantRepo := NewTenantRepository(db)
	memoryRepo := NewMemoryRepository(db)
	entityRepo := NewEntityFactRepository(db)

	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant One"})
	require.NoError(t, err)
	m1, err := memoryRepo.Store(ctx, domain.Memory{
		TenantID: "tenant_1",
		Content:  "Melanie enjoys camping.",
	})
	require.NoError(t, err)
	m2, err := memoryRepo.Store(ctx, domain.Memory{
		TenantID: "tenant_1",
		Content:  "Melanie practices pottery.",
	})
	require.NoError(t, err)

	_, err = entityRepo.Store(ctx, domain.EntityFact{
		TenantID:    "tenant_1",
		Entity:      "Melanie",
		Relation:    "Activity",
		RelationRaw: "likes",
		Value:       "camping",
		MemoryID:    m1.ID,
		CreatedAt:   time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	_, err = entityRepo.Store(ctx, domain.EntityFact{
		TenantID:    "tenant_1",
		Entity:      "melanie",
		Relation:    "activity",
		RelationRaw: "uses_tool",
		Value:       "pottery",
		MemoryID:    m2.ID,
		CreatedAt:   time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	facts, err := entityRepo.ListByEntityRelation(ctx, "tenant_1", "melanie", "activity", 10)
	require.NoError(t, err)
	require.Len(t, facts, 2)
	require.Equal(t, "pottery", facts[0].Value)
	require.Equal(t, "uses_tool", facts[0].RelationRaw)
	require.Equal(t, m2.ID, facts[0].MemoryID)
	require.Equal(t, "camping", facts[1].Value)
	require.Equal(t, "likes", facts[1].RelationRaw)
	require.Equal(t, m1.ID, facts[1].MemoryID)
}

func TestEntityFactRepositoryStoreBatchDedupes(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

	tenantRepo := NewTenantRepository(db)
	memoryRepo := NewMemoryRepository(db)
	entityRepo := NewEntityFactRepository(db)

	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant One"})
	require.NoError(t, err)
	m1, err := memoryRepo.Store(ctx, domain.Memory{
		TenantID: "tenant_1",
		Content:  "Melanie enjoys camping.",
	})
	require.NoError(t, err)

	_, err = entityRepo.StoreBatch(ctx, []domain.EntityFact{
		{
			TenantID:    "tenant_1",
			Entity:      "Melanie",
			Relation:    "activity",
			RelationRaw: "likes",
			Value:       "camping",
			MemoryID:    m1.ID,
		},
		{
			TenantID:    "tenant_1",
			Entity:      "melanie",
			Relation:    "activity",
			RelationRaw: "enjoys",
			Value:       "camping",
			MemoryID:    m1.ID,
		},
	})
	require.NoError(t, err)

	facts, err := entityRepo.ListByEntityRelation(ctx, "tenant_1", "melanie", "activity", 10)
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, "enjoys", facts[0].RelationRaw)
}
