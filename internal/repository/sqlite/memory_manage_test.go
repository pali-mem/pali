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

func newManageTestRepos(t *testing.T) (*MemoryRepository, *TenantRepository) {
	t.Helper()
	ctx := context.Background()
	db, err := Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(db) })

	tenantRepo := NewTenantRepository(db)
	for _, id := range []string{"tenant_a", "tenant_b"} {
		_, err := tenantRepo.Create(ctx, domain.Tenant{ID: id, Name: id})
		require.NoError(t, err)
	}
	return NewMemoryRepository(db), tenantRepo
}

func TestMemoryRepositoryListPageOrderingAndFilters(t *testing.T) {
	ctx := context.Background()
	memRepo, _ := newManageTestRepos(t)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		tier := domain.MemoryTierEpisodic
		kind := domain.MemoryKindRawTurn
		if i%2 == 1 {
			tier = domain.MemoryTierSemantic
			kind = domain.MemoryKindObservation
		}
		// Sub-second offsets with trailing zeros exercise the timestamp ordering.
		stored, err := memRepo.Store(ctx, domain.Memory{
			TenantID:  "tenant_a",
			Content:   fmt.Sprintf("memory number %d", i),
			Tier:      tier,
			Kind:      kind,
			CreatedAt: base.Add(time.Duration(i) * 500 * time.Millisecond),
		})
		require.NoError(t, err)
		ids = append(ids, stored.ID)
	}
	_, err := memRepo.Store(ctx, domain.Memory{TenantID: "tenant_b", Content: "other tenant"})
	require.NoError(t, err)

	page, err := memRepo.ListPage(ctx, "tenant_a", domain.MemoryListFilter{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.Equal(t, ids[4], page[0].ID)
	require.Equal(t, ids[3], page[1].ID)

	page, err = memRepo.ListPage(ctx, "tenant_a", domain.MemoryListFilter{Limit: 10, Offset: 3})
	require.NoError(t, err)
	require.Len(t, page, 2)
	require.Equal(t, ids[1], page[0].ID)
	require.Equal(t, ids[0], page[1].ID)

	page, err = memRepo.ListPage(ctx, "tenant_a", domain.MemoryListFilter{
		Limit: 10,
		Tiers: []domain.MemoryTier{domain.MemoryTierSemantic},
	})
	require.NoError(t, err)
	require.Len(t, page, 2)

	page, err = memRepo.ListPage(ctx, "tenant_a", domain.MemoryListFilter{
		Limit: 10,
		Kinds: []domain.MemoryKind{domain.MemoryKindRawTurn},
		Tiers: []domain.MemoryTier{domain.MemoryTierSemantic},
	})
	require.NoError(t, err)
	require.Empty(t, page)

	_, err = memRepo.ListPage(ctx, "", domain.MemoryListFilter{})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestMemoryRepositoryUpdateRefreshesFTSAndScopesTenant(t *testing.T) {
	ctx := context.Background()
	memRepo, _ := newManageTestRepos(t)

	stored, err := memRepo.Store(ctx, domain.Memory{
		TenantID:   "tenant_a",
		Content:    "User likes zebrafish",
		Tier:       domain.MemoryTierEpisodic,
		Tags:       []string{"old"},
		Importance: 0.2,
	})
	require.NoError(t, err)

	hits, err := memRepo.Search(ctx, "tenant_a", "zebrafish", 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)

	newContent := "User likes narwhals"
	tier := domain.MemoryTierSemantic
	tags := []string{"new", "tags"}
	importance := 0.9
	updated, err := memRepo.Update(ctx, "tenant_a", stored.ID, domain.MemoryPatch{
		Content:    &newContent,
		Tags:       &tags,
		Tier:       &tier,
		Importance: &importance,
	})
	require.NoError(t, err)
	require.Equal(t, newContent, updated.Content)
	require.Equal(t, domain.MemoryTierSemantic, updated.Tier)
	require.Equal(t, tags, updated.Tags)
	require.InDelta(t, 0.9, updated.Importance, 0.0001)
	require.True(t, updated.UpdatedAt.After(stored.UpdatedAt) || updated.UpdatedAt.Equal(stored.UpdatedAt))
	require.True(t, updated.CreatedAt.Equal(stored.CreatedAt))

	hits, err = memRepo.Search(ctx, "tenant_a", "zebrafish", 5)
	require.NoError(t, err)
	require.Empty(t, hits)
	hits, err = memRepo.Search(ctx, "tenant_a", "narwhals", 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	require.Equal(t, stored.ID, hits[0].ID)

	// Metadata-only patch keeps the content and the FTS row intact.
	emptyTags := []string{}
	updated, err = memRepo.Update(ctx, "tenant_a", stored.ID, domain.MemoryPatch{Tags: &emptyTags})
	require.NoError(t, err)
	require.Equal(t, newContent, updated.Content)
	require.Empty(t, updated.Tags)
	hits, err = memRepo.Search(ctx, "tenant_a", "narwhals", 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)

	_, err = memRepo.Update(ctx, "tenant_b", stored.ID, domain.MemoryPatch{Tags: &tags})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = memRepo.Update(ctx, "tenant_a", "mem_missing", domain.MemoryPatch{Tags: &tags})
	require.ErrorIs(t, err, domain.ErrNotFound)
}

func TestTenantRepositoryGetAndMemoryStats(t *testing.T) {
	ctx := context.Background()
	memRepo, tenantRepo := newManageTestRepos(t)

	tenant, err := tenantRepo.Get(ctx, "tenant_a")
	require.NoError(t, err)
	require.Equal(t, "tenant_a", tenant.ID)
	_, err = tenantRepo.Get(ctx, "tenant_missing")
	require.ErrorIs(t, err, domain.ErrNotFound)

	empty, err := tenantRepo.MemoryStats(ctx, "tenant_a")
	require.NoError(t, err)
	require.EqualValues(t, 0, empty.MemoryCount)
	require.True(t, empty.NewestMemoryAt.IsZero())

	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	seed := []domain.Memory{
		{Content: "one", Tier: domain.MemoryTierEpisodic, Kind: domain.MemoryKindRawTurn, RecallCount: 2, CreatedAt: base},
		{Content: "two", Tier: domain.MemoryTierSemantic, Kind: domain.MemoryKindObservation, RecallCount: 3, CreatedAt: base.Add(time.Hour)},
		{Content: "three", Tier: domain.MemoryTierSemantic, Kind: domain.MemoryKindRawTurn, CreatedAt: base.Add(30 * time.Minute)},
	}
	for _, m := range seed {
		m.TenantID = "tenant_a"
		_, err := memRepo.Store(ctx, m)
		require.NoError(t, err)
	}

	stats, err := tenantRepo.MemoryStats(ctx, "tenant_a")
	require.NoError(t, err)
	require.EqualValues(t, 3, stats.MemoryCount)
	require.Equal(t, map[string]int64{"episodic": 1, "semantic": 2}, stats.ByTier)
	require.Equal(t, map[string]int64{"raw_turn": 2, "observation": 1}, stats.ByKind)
	require.EqualValues(t, 5, stats.TotalRecalls)
	require.True(t, stats.NewestMemoryAt.Equal(base.Add(time.Hour)))
}

func TestMemoryRepositoryUpdateClearsStaleQueryView(t *testing.T) {
	ctx := context.Background()
	memRepo, _ := newManageTestRepos(t)

	stored, err := memRepo.Store(ctx, domain.Memory{
		TenantID:      "tenant_a",
		Content:       "Caroline adopted a beagle",
		QueryViewText: "what pet did caroline adopt beagle",
		Tier:          domain.MemoryTierSemantic,
		Kind:          domain.MemoryKindObservation,
	})
	require.NoError(t, err)

	content := "Caroline moved to Boston"
	updated, err := memRepo.Update(ctx, "tenant_a", stored.ID, domain.MemoryPatch{Content: &content})
	require.NoError(t, err)
	require.Empty(t, updated.QueryViewText)

	hits, err := memRepo.Search(ctx, "tenant_a", "beagle", 10)
	require.NoError(t, err)
	require.Empty(t, hits, "old query view must not stay searchable after a content update")

	tags := []string{"moved"}
	again, err := memRepo.Update(ctx, "tenant_a", stored.ID, domain.MemoryPatch{Tags: &tags})
	require.NoError(t, err)
	require.Equal(t, content, again.Content)
}
