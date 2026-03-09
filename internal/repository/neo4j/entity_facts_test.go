package neo4j

import (
	"context"
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/pali-mem/pali/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestPrepareEntityFactForStore_NormalizesAndDefaults(t *testing.T) {
	now := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	fact := domain.EntityFact{
		TenantID:    " tenant_1 ",
		Entity:      "  Alice   Johnson ",
		Relation:    "  Favorite  Color ",
		RelationRaw: "  profile_attribute  ",
		Value:       "  bright    blue ",
		MemoryID:    "  mem_123  ",
	}

	err := prepareEntityFactForStore(&fact, now)
	require.NoError(t, err)

	require.Equal(t, "tenant_1", fact.TenantID)
	require.Equal(t, "alice johnson", fact.Entity)
	require.Equal(t, "favorite color", fact.Relation)
	require.Equal(t, "profile_attribute", fact.RelationRaw)
	require.Equal(t, "bright blue", fact.Value)
	require.Equal(t, "mem_123", fact.MemoryID)
	require.NotEmpty(t, fact.ID)
	require.Contains(t, fact.ID, "ef_")
	require.True(t, fact.CreatedAt.Equal(now))
}

func TestPrepareEntityFactForStore_PreservesProvidedIDAndCreatedAt(t *testing.T) {
	now := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	created := time.Date(2024, 2, 1, 12, 30, 0, 0, time.UTC)
	fact := domain.EntityFact{
		ID:          "ef_custom",
		TenantID:    "tenant_1",
		Entity:      "alice",
		Relation:    "role",
		RelationRaw: "job_title",
		Value:       "engineer",
		CreatedAt:   created,
	}

	err := prepareEntityFactForStore(&fact, now)
	require.NoError(t, err)
	require.Equal(t, "ef_custom", fact.ID)
	require.Equal(t, "job_title", fact.RelationRaw)
	require.True(t, fact.CreatedAt.Equal(created))
}

func TestPrepareEntityFactForStore_DefaultsRawRelationToCanonical(t *testing.T) {
	now := time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC)
	fact := domain.EntityFact{
		TenantID: "tenant_1",
		Entity:   "alice",
		Relation: "role",
		Value:    "engineer",
	}

	err := prepareEntityFactForStore(&fact, now)
	require.NoError(t, err)
	require.Equal(t, "role", fact.RelationRaw)
}

func TestPrepareEntityFactForStore_InvalidInput(t *testing.T) {
	now := time.Now().UTC()

	require.ErrorIs(t, prepareEntityFactForStore(nil, now), domain.ErrInvalidInput)

	cases := []domain.EntityFact{
		{TenantID: "", Entity: "alice", Relation: "role", Value: "engineer"},
		{TenantID: "tenant_1", Entity: "", Relation: "role", Value: "engineer"},
		{TenantID: "tenant_1", Entity: "alice", Relation: "", Value: "engineer"},
		{TenantID: "tenant_1", Entity: "alice", Relation: "role", Value: ""},
	}
	for _, c := range cases {
		fact := c
		err := prepareEntityFactForStore(&fact, now)
		require.ErrorIs(t, err, domain.ErrInvalidInput)
	}
}

func TestStoreBatch_EmptyBatchNoop(t *testing.T) {
	repo := &EntityFactRepository{}
	out, err := repo.StoreBatch(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, out)

	out, err = repo.StoreBatch(context.Background(), []domain.EntityFact{})
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestStoreBatch_UninitializedRepo(t *testing.T) {
	repo := &EntityFactRepository{}
	_, err := repo.StoreBatch(context.Background(), []domain.EntityFact{
		{
			TenantID: "tenant_1",
			Entity:   "alice",
			Relation: "role",
			Value:    "engineer",
		},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not initialized")
}

func TestStore_UninitializedRepo(t *testing.T) {
	repo := &EntityFactRepository{}
	_, err := repo.Store(context.Background(), domain.EntityFact{
		TenantID: "tenant_1",
		Entity:   "alice",
		Relation: "role",
		Value:    "engineer",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "not initialized")
}

func TestListByEntityRelation_ValidationAndDefaults(t *testing.T) {
	repo := &EntityFactRepository{}

	_, err := repo.ListByEntityRelation(context.Background(), "", "alice", "role", 10)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	_, err = repo.ListByEntityRelation(context.Background(), "tenant_1", "", "role", 10)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	_, err = repo.ListByEntityRelation(context.Background(), "tenant_1", "alice", "", 10)
	require.ErrorIs(t, err, domain.ErrInvalidInput)

	// Valid keys but uninitialized driver should fail after normalization/defaulting.
	_, err = repo.ListByEntityRelation(context.Background(), " tenant_1 ", " Alice ", "  ROLE ", 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not initialized")
}

func TestNormalizeEntityFactHelpers(t *testing.T) {
	require.Equal(t, "alice johnson", normalizeEntityFactKey("  Alice   JOHNSON  "))
	require.Equal(t, "favorite color", normalizeEntityFactKey(" Favorite   Color "))
	require.Equal(t, "bright blue", normalizeEntityFactValue(" bright   blue "))
	require.Equal(t, "", normalizeEntityFactValue("   "))
}

func TestScanEntityFactRecord_Success(t *testing.T) {
	now := time.Date(2026, 3, 8, 10, 30, 0, 0, time.UTC)
	record := &neo4j.Record{
		Keys: []string{
			"id",
			"tenant_id",
			"entity",
			"relation",
			"relation_raw",
			"value",
			"memory_id",
			"created_at_ns",
		},
		Values: []any{
			"ef_123",
			"tenant_1",
			"alice",
			"role",
			"job_title",
			"engineer",
			"mem_1",
			now.UnixNano(),
		},
	}

	fact, err := scanEntityFactRecord(record)
	require.NoError(t, err)
	require.Equal(t, "ef_123", fact.ID)
	require.Equal(t, "tenant_1", fact.TenantID)
	require.Equal(t, "alice", fact.Entity)
	require.Equal(t, "role", fact.Relation)
	require.Equal(t, "job_title", fact.RelationRaw)
	require.Equal(t, "engineer", fact.Value)
	require.Equal(t, "mem_1", fact.MemoryID)
	require.True(t, fact.CreatedAt.Equal(now))
}

func TestScanEntityFactRecord_MissingRequiredColumns(t *testing.T) {
	makeRecord := func(keys []string, values []any) *neo4j.Record {
		return &neo4j.Record{Keys: keys, Values: values}
	}

	_, err := scanEntityFactRecord(makeRecord(
		[]string{"tenant_id", "entity", "relation", "value"},
		[]any{"tenant_1", "alice", "role", "engineer"},
	))
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing id")

	_, err = scanEntityFactRecord(makeRecord(
		[]string{"id", "entity", "relation", "value"},
		[]any{"ef_1", "alice", "role", "engineer"},
	))
	require.Error(t, err)
	require.Contains(t, err.Error(), "missing tenant_id")
}

func TestAsInt64AndMinInt(t *testing.T) {
	v, ok := asInt64(int64(7))
	require.True(t, ok)
	require.EqualValues(t, 7, v)
	v, ok = asInt64(float64(12))
	require.True(t, ok)
	require.EqualValues(t, 12, v)
	_, ok = asInt64("12")
	require.False(t, ok)

	require.Equal(t, 3, minInt(3, 9))
	require.Equal(t, 2, minInt(7, 2))
}

func TestSchemaStatementsCoverMergeKeysAndLookup(t *testing.T) {
	require.Len(t, schemaStatements, 4)
	require.Contains(t, schemaStatements[0], "PaliEntity")
	require.Contains(t, schemaStatements[0], "IS UNIQUE")
	require.Contains(t, schemaStatements[1], "PaliEntityFact")
	require.Contains(t, schemaStatements[1], "memory_id")
	require.Contains(t, schemaStatements[2], "PaliMemory")
	require.Contains(t, schemaStatements[3], "created_at_ns")
	require.Contains(t, schemaStatements[3], "CREATE INDEX")
}
