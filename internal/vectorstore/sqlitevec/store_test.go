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

func TestStoreReadsAndMigratesLegacyJSONEmbeddings(t *testing.T) {
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
	for _, id := range []string{"m1", "m2"} {
		_, err = memoryRepo.Store(ctx, domain.Memory{ID: id, TenantID: "tenant_1", Content: id, Tier: domain.MemoryTierSemantic})
		require.NoError(t, err)
	}

	// m1 is a row written before the blob column existed; m2 uses the blob.
	_, err = db.ExecContext(ctx, `INSERT INTO memory_embeddings (tenant_id, memory_id, embedding_json, updated_at) VALUES (?, ?, ?, ?)`,
		"tenant_1", "m1", "[0.25,-1.5,3]", "2026-01-01T00:00:00Z")
	require.NoError(t, err)
	store := NewStore(db)
	require.NoError(t, store.Upsert(ctx, "tenant_1", "m2", []float32{-3, 1.5, -0.25}))

	query := []float32{0.25, -1.5, 3}
	before, err := store.Search(ctx, "tenant_1", query, 2)
	require.NoError(t, err)
	require.Len(t, before, 2)
	require.Equal(t, "m1", before[0].MemoryID)
	require.InDelta(t, 1.0, before[0].Similarity, 1e-9)

	require.NoError(t, store.MigrateLegacyEmbeddings(ctx))
	require.NoError(t, store.MigrateLegacyEmbeddings(ctx))
	var legacy int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_embeddings WHERE embedding_blob IS NULL`).Scan(&legacy))
	require.Zero(t, legacy)

	after, err := store.Search(ctx, "tenant_1", query, 2)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestEncodeEmbeddingRoundTrip(t *testing.T) {
	in := []float32{0, 1, -1, 3.1415927, -2.5e-8, 65504}
	out, err := decodeStoredEmbedding(encodeEmbedding(in), "", nil)
	require.NoError(t, err)
	require.Equal(t, in, out)

	_, err = decodeStoredEmbedding([]byte{1, 2, 3}, "", nil)
	require.Error(t, err)
}
