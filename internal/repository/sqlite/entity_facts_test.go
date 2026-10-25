package sqlite

import (
	"context"
	"fmt"
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
	defer closeDB(db)

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
	defer closeDB(db)

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

func TestEntityFactRepositoryInvalidateEntityRelation(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer closeDB(db)

	tenantRepo := NewTenantRepository(db)
	memoryRepo := NewMemoryRepository(db)
	entityRepo := NewEntityFactRepository(db)

	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant One"})
	require.NoError(t, err)
	m1, err := memoryRepo.Store(ctx, domain.Memory{TenantID: "tenant_1", Content: "Melanie is married."})
	require.NoError(t, err)
	m2, err := memoryRepo.Store(ctx, domain.Memory{TenantID: "tenant_1", Content: "Melanie is single."})
	require.NoError(t, err)

	oldFact, err := entityRepo.Store(ctx, domain.EntityFact{
		ID:         "ef_old",
		TenantID:   "tenant_1",
		Entity:     "melanie",
		Relation:   "relationship status",
		Value:      "married",
		MemoryID:   m1.ID,
		CreatedAt:  time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		ObservedAt: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		ValidFrom:  time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	_, err = entityRepo.Store(ctx, domain.EntityFact{
		ID:         "ef_new",
		TenantID:   "tenant_1",
		Entity:     "melanie",
		Relation:   "relationship status",
		Value:      "single",
		MemoryID:   m2.ID,
		CreatedAt:  time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
		ObservedAt: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
		ValidFrom:  time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)

	cutoff := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	err = entityRepo.InvalidateEntityRelation(ctx, "tenant_1", "melanie", "relationship status", "single", "ef_new", cutoff)
	require.NoError(t, err)

	facts, err := entityRepo.ListByEntityRelation(ctx, "tenant_1", "melanie", "relationship status", 10)
	require.NoError(t, err)
	require.Len(t, facts, 2)
	require.Equal(t, "single", facts[0].Value)
	require.Empty(t, facts[0].InvalidatedByFactID)
	require.Nil(t, facts[0].ValidTo)
	require.Equal(t, oldFact.ID, facts[1].ID)
	require.Equal(t, "ef_new", facts[1].InvalidatedByFactID)
	require.NotNil(t, facts[1].ValidTo)
	require.True(t, facts[1].ValidTo.Equal(cutoff))
}

// ListByEntityRelations must return exactly the concatenation of per-pair
// ListByEntityRelation results, including per-pair limits and the order of
// facts that share created_at.
func TestEntityFactRepositoryListByEntityRelationsMatchesPerPairLookups(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer closeDB(db)

	tenantRepo := NewTenantRepository(db)
	memoryRepo := NewMemoryRepository(db)
	entityRepo := NewEntityFactRepository(db)
	for _, tenantID := range []string{"tenant_1", "tenant_2"} {
		_, err = tenantRepo.Create(ctx, domain.Tenant{ID: tenantID, Name: tenantID})
		require.NoError(t, err)
	}
	memoryIDs := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		m, err := memoryRepo.Store(ctx, domain.Memory{TenantID: "tenant_1", Content: fmt.Sprintf("memory %d", i)})
		require.NoError(t, err)
		memoryIDs = append(memoryIDs, m.ID)
	}
	other, err := memoryRepo.Store(ctx, domain.Memory{TenantID: "tenant_2", Content: "other tenant memory"})
	require.NoError(t, err)

	entities := make([]string, 0, 12)
	for i := 0; i < 12; i++ {
		entities = append(entities, fmt.Sprintf("person %02d", i))
	}
	relations := []string{"activity", "preference", "event", "plan", "goal", "identity", "role", "relationship", "relationship status", "place", "book"}
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	facts := make([]domain.EntityFact, 0, 400)
	for ei, entity := range entities {
		for ri, relation := range relations {
			n := (ei + ri) % 6
			for k := 0; k < n; k++ {
				fact := domain.EntityFact{
					TenantID: "tenant_1",
					Entity:   entity,
					Relation: relation,
					// Values inserted in descending order so neither value nor
					// insertion order lines up with created_at.
					Value:    fmt.Sprintf("value %02d", 20-k),
					MemoryID: memoryIDs[(ei+ri+k)%len(memoryIDs)],
				}
				if k%3 == 2 {
					fact.CreatedAt = base.Add(time.Duration(ei+k) * time.Minute)
				}
				facts = append(facts, fact)
			}
		}
	}
	// Facts without CreatedAt share the batch timestamp, which creates ties.
	_, err = entityRepo.StoreBatch(ctx, facts)
	require.NoError(t, err)
	_, err = entityRepo.Store(ctx, domain.EntityFact{TenantID: "tenant_2", Entity: entities[0], Relation: "activity", Value: "leak", MemoryID: other.ID})
	require.NoError(t, err)

	// Includes a duplicate entity in a different case and > 100 distinct pairs.
	query := append([]string{"Person 01"}, entities...)
	for _, limit := range []int{0, 1, 3} {
		var want []domain.EntityFact
		for _, entity := range query {
			for _, relation := range relations {
				pairFacts, err := entityRepo.ListByEntityRelation(ctx, "tenant_1", entity, relation, limit)
				require.NoError(t, err)
				want = append(want, pairFacts...)
			}
		}
		got, err := entityRepo.ListByEntityRelations(ctx, "tenant_1", query, relations, limit)
		require.NoError(t, err)
		require.NotEmpty(t, got)
		require.Equal(t, want, got, "limit=%d", limit)
	}

	empty, err := entityRepo.ListByEntityRelations(ctx, "tenant_1", nil, relations, 3)
	require.NoError(t, err)
	require.Empty(t, empty)
	_, err = entityRepo.ListByEntityRelations(ctx, " ", entities, relations, 3)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	_, err = entityRepo.ListByEntityRelations(ctx, "tenant_1", []string{"person 00", "  "}, relations, 3)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}
