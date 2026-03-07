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
		TenantID:         "tenant_1",
		Content:          "User prefers Go for backend systems",
		QueryViewText:    "what stack does the user like for backend work",
		Tier:             domain.MemoryTierSemantic,
		Tags:             []string{"preferences", "golang"},
		Source:           "seed",
		CreatedBy:        domain.MemoryCreatedByUser,
		Kind:             domain.MemoryKindObservation,
		CanonicalKey:     "canon_1",
		SourceTurnHash:   "turn_hash_1",
		SourceFactIndex:  2,
		Extractor:        "ollama",
		ExtractorVersion: "qwen3:4b",
		Importance:       0.77,
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
	require.Equal(t, "canon_1", results[0].CanonicalKey)
	require.Equal(t, "turn_hash_1", results[0].SourceTurnHash)
	require.Equal(t, 2, results[0].SourceFactIndex)
	require.Equal(t, "ollama", results[0].Extractor)
	require.Equal(t, "qwen3:4b", results[0].ExtractorVersion)
	require.Equal(t, "what stack does the user like for backend work", results[0].QueryViewText)
	require.Equal(t, 0, results[0].RecallCount)

	byID, err := memRepo.GetByIDs(ctx, "tenant_1", []string{stored.ID})
	require.NoError(t, err)
	require.Len(t, byID, 1)
	require.Equal(t, stored.ID, byID[0].ID)
	require.Equal(t, "seed", byID[0].Source)
	require.Equal(t, domain.MemoryCreatedByUser, byID[0].CreatedBy)
	require.Equal(t, domain.MemoryKindObservation, byID[0].Kind)
	require.Equal(t, "canon_1", byID[0].CanonicalKey)
	require.Equal(t, "turn_hash_1", byID[0].SourceTurnHash)
	require.Equal(t, 2, byID[0].SourceFactIndex)
	require.Equal(t, "ollama", byID[0].Extractor)
	require.Equal(t, "qwen3:4b", byID[0].ExtractorVersion)
	require.Equal(t, "what stack does the user like for backend work", byID[0].QueryViewText)
	require.Equal(t, 0, byID[0].RecallCount)

	byCanonicalKey, err := memRepo.FindByCanonicalKey(ctx, "tenant_1", "canon_1")
	require.NoError(t, err)
	require.NotNil(t, byCanonicalKey)
	require.Equal(t, stored.ID, byCanonicalKey.ID)

	bySourceTurn, err := memRepo.ListBySourceTurnHash(ctx, "tenant_1", "turn_hash_1", 10)
	require.NoError(t, err)
	require.Len(t, bySourceTurn, 1)
	require.Equal(t, stored.ID, bySourceTurn[0].ID)

	aliasResults, err := memRepo.Search(ctx, "tenant_1", "stack backend work like", 10)
	require.NoError(t, err)
	require.Len(t, aliasResults, 1)
	require.Equal(t, stored.ID, aliasResults[0].ID)

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

func TestMemoryRepositoryIndexJobLifecycle(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	defer db.Close()

	tenantRepo := NewTenantRepository(db)
	memRepo := NewMemoryRepository(db)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_1", Name: "Tenant One"})
	require.NoError(t, err)

	stored, err := memRepo.Store(ctx, domain.Memory{
		TenantID: "tenant_1",
		Content:  "User likes tea.",
	})
	require.NoError(t, err)
	require.NotEmpty(t, stored.ID)

	type indexJobRow struct {
		state     string
		lastError string
		attempts  int
	}
	readIndexJob := func(memoryID string, op domain.MemoryIndexOperation) indexJobRow {
		t.Helper()
		var row indexJobRow
		err := db.QueryRowContext(
			ctx,
			`SELECT state, last_error, attempts
			 FROM memory_index_jobs
			 WHERE tenant_id = ? AND memory_id = ? AND op = ?`,
			"tenant_1",
			memoryID,
			string(op),
		).Scan(&row.state, &row.lastError, &row.attempts)
		require.NoError(t, err)
		return row
	}

	upsertJob := readIndexJob(stored.ID, domain.MemoryIndexOperationUpsert)
	require.Equal(t, string(domain.MemoryIndexStatePending), upsertJob.state)
	require.Equal(t, "", upsertJob.lastError)
	require.Equal(t, 0, upsertJob.attempts)

	require.NoError(t, memRepo.MarkIndexState(
		ctx,
		"tenant_1",
		[]string{stored.ID},
		domain.MemoryIndexOperationUpsert,
		domain.MemoryIndexStateIndexed,
		"",
	))
	upsertJob = readIndexJob(stored.ID, domain.MemoryIndexOperationUpsert)
	require.Equal(t, string(domain.MemoryIndexStateIndexed), upsertJob.state)
	require.Equal(t, "", upsertJob.lastError)
	require.Equal(t, 0, upsertJob.attempts)

	require.NoError(t, memRepo.MarkIndexState(
		ctx,
		"tenant_1",
		[]string{stored.ID},
		domain.MemoryIndexOperationUpsert,
		domain.MemoryIndexStateFailed,
		"vector timeout",
	))
	upsertJob = readIndexJob(stored.ID, domain.MemoryIndexOperationUpsert)
	require.Equal(t, string(domain.MemoryIndexStateFailed), upsertJob.state)
	require.Equal(t, "vector timeout", upsertJob.lastError)
	require.Equal(t, 1, upsertJob.attempts)

	require.NoError(t, memRepo.Delete(ctx, "tenant_1", stored.ID))
	deleteJob := readIndexJob(stored.ID, domain.MemoryIndexOperationDelete)
	require.Equal(t, string(domain.MemoryIndexStatePending), deleteJob.state)

	require.NoError(t, memRepo.MarkIndexState(
		ctx,
		"tenant_1",
		[]string{stored.ID},
		domain.MemoryIndexOperationDelete,
		domain.MemoryIndexStateTombstoned,
		"",
	))
	deleteJob = readIndexJob(stored.ID, domain.MemoryIndexOperationDelete)
	require.Equal(t, string(domain.MemoryIndexStateTombstoned), deleteJob.state)
}
