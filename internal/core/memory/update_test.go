package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	sqlitevec "github.com/pali-mem/pali/internal/vectorstore/sqlitevec"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

// textEmbedder embeds text as a single value derived from its length and
// records every input so tests can assert what was embedded.
type textEmbedder struct {
	mu    sync.Mutex
	texts []string
	err   error
}

func (e *textEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return nil, e.err
	}
	e.texts = append(e.texts, text)
	return []float32{float32(len(text))}, nil
}

type recordingVectorStore struct {
	domain.VectorStore
	mu      sync.Mutex
	upserts []string
	err     error
}

func (v *recordingVectorStore) Upsert(ctx context.Context, tenantID, memoryID string, embedding []float32) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.err != nil {
		return v.err
	}
	v.upserts = append(v.upserts, fmt.Sprintf("%s/%s/%v", tenantID, memoryID, embedding))
	return v.VectorStore.Upsert(ctx, tenantID, memoryID, embedding)
}

type updateFixture struct {
	svc      *Service
	embedder *textEmbedder
	vector   *recordingVectorStore
	memRepo  *sqliterepo.MemoryRepository
}

func newUpdateFixture(t *testing.T) updateFixture {
	t.Helper()
	ctx := context.Background()
	db, err := sqliterepo.Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	t.Cleanup(func() { sqliterepo.CloseDBForTest(db) })

	tenantRepo := sqliterepo.NewTenantRepository(db)
	for _, id := range []string{"tenant_a", "tenant_b"} {
		_, err := tenantRepo.Create(ctx, domain.Tenant{ID: id, Name: id})
		require.NoError(t, err)
	}
	memRepo := sqliterepo.NewMemoryRepository(db)
	embedder := &textEmbedder{}
	vector := &recordingVectorStore{VectorStore: sqlitevec.NewStore(db)}
	return updateFixture{
		svc:      NewService(memRepo, tenantRepo, vector, embedder, scorerStub{}),
		embedder: embedder,
		vector:   vector,
		memRepo:  memRepo,
	}
}

func (f updateFixture) store(t *testing.T, tenantID, content string) domain.Memory {
	t.Helper()
	m, err := f.svc.Store(context.Background(), StoreInput{TenantID: tenantID, Content: content, Tier: domain.MemoryTierEpisodic})
	require.NoError(t, err)
	return m
}

func TestUpdateContentReindexesLexicalAndVector(t *testing.T) {
	ctx := context.Background()
	f := newUpdateFixture(t)
	stored := f.store(t, "tenant_a", "User likes zebrafish")
	f.embedder.texts = nil
	f.vector.upserts = nil

	newContent := "User likes narwhals a lot"
	updated, err := f.svc.Update(ctx, "tenant_a", stored.ID, UpdateInput{Content: &newContent})
	require.NoError(t, err)
	require.Equal(t, newContent, updated.Content)

	require.Equal(t, []string{newContent}, f.embedder.texts)
	require.Equal(t, []string{fmt.Sprintf("tenant_a/%s/[%d]", stored.ID, len(newContent))}, f.vector.upserts)

	old, err := f.memRepo.Search(ctx, "tenant_a", "zebrafish", 5)
	require.NoError(t, err)
	require.Empty(t, old)
	fresh, err := f.memRepo.Search(ctx, "tenant_a", "narwhals", 5)
	require.NoError(t, err)
	require.Len(t, fresh, 1)
	require.Equal(t, stored.ID, fresh[0].ID)

	// The raw-turn identity follows the new text.
	require.Equal(t, buildRawTurnIdentity(newContent).CanonicalKey, updated.CanonicalKey)
	require.Equal(t, buildRawTurnIdentity(newContent).SourceTurnHash, updated.SourceTurnHash)
}

func TestUpdateMetadataOnlySkipsEmbedding(t *testing.T) {
	ctx := context.Background()
	f := newUpdateFixture(t)
	stored := f.store(t, "tenant_a", "plain memory")
	f.embedder.texts = nil
	f.vector.upserts = nil

	tags := []string{"a", "b"}
	tier := domain.MemoryTierSemantic
	importance := 0.8
	updated, err := f.svc.Update(ctx, "tenant_a", stored.ID, UpdateInput{Tags: &tags, Tier: &tier, Importance: &importance})
	require.NoError(t, err)
	require.Equal(t, tags, updated.Tags)
	require.Equal(t, domain.MemoryTierSemantic, updated.Tier)
	require.InDelta(t, 0.8, updated.Importance, 0.0001)
	require.Equal(t, "plain memory", updated.Content)
	require.Empty(t, f.embedder.texts)
	require.Empty(t, f.vector.upserts)
}

func TestUpdateAutoTierResolvesLikeStore(t *testing.T) {
	ctx := context.Background()
	f := newUpdateFixture(t)
	stored := f.store(t, "tenant_a", "plain memory")

	auto := domain.MemoryTierAuto
	content := "I prefer dark mode"
	updated, err := f.svc.Update(ctx, "tenant_a", stored.ID, UpdateInput{Tier: &auto, Content: &content})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryTierSemantic, updated.Tier)
}

func TestUpdateValidationAndScoping(t *testing.T) {
	ctx := context.Background()
	f := newUpdateFixture(t)
	stored := f.store(t, "tenant_a", "scoped memory")

	blank := "   "
	badTier := domain.MemoryTier("bogus")
	high := 1.5
	tags := []string{"x"}
	for name, in := range map[string]UpdateInput{
		"empty":      {},
		"blank":      {Content: &blank},
		"bad tier":   {Tier: &badTier},
		"importance": {Importance: &high},
	} {
		_, err := f.svc.Update(ctx, "tenant_a", stored.ID, in)
		require.ErrorIs(t, err, domain.ErrInvalidInput, name)
	}

	_, err := f.svc.Update(ctx, "tenant_a", "mem_missing", UpdateInput{Tags: &tags})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = f.svc.Update(ctx, "tenant_b", stored.ID, UpdateInput{Tags: &tags})
	require.ErrorIs(t, err, domain.ErrNotFound)
	_, err = f.svc.Update(ctx, "tenant_missing", stored.ID, UpdateInput{Tags: &tags})
	require.ErrorIs(t, err, domain.ErrNotFound)

	got, err := f.svc.Get(ctx, "tenant_a", stored.ID)
	require.NoError(t, err)
	require.Equal(t, "scoped memory", got.Content)
}

func TestUpdateEmbedFailureLeavesMemoryUntouched(t *testing.T) {
	ctx := context.Background()
	f := newUpdateFixture(t)
	stored := f.store(t, "tenant_a", "original text")

	f.embedder.err = errors.New("embedder down")
	content := "changed text"
	_, err := f.svc.Update(ctx, "tenant_a", stored.ID, UpdateInput{Content: &content})
	require.Error(t, err)

	got, err := f.svc.Get(ctx, "tenant_a", stored.ID)
	require.NoError(t, err)
	require.Equal(t, "original text", got.Content)
	hits, err := f.memRepo.Search(ctx, "tenant_a", "original", 5)
	require.NoError(t, err)
	require.Len(t, hits, 1)
}

func TestUpdateVectorFailureIsReportedAndRetryable(t *testing.T) {
	ctx := context.Background()
	f := newUpdateFixture(t)
	stored := f.store(t, "tenant_a", "original text")

	f.vector.err = errors.New("vector backend down")
	content := "changed text"
	_, err := f.svc.Update(ctx, "tenant_a", stored.ID, UpdateInput{Content: &content})
	require.ErrorContains(t, err, "vector reindex failed")

	// Retrying the same content re-embeds and upserts even though the row already matches.
	f.vector.err = nil
	f.vector.upserts = nil
	_, err = f.svc.Update(ctx, "tenant_a", stored.ID, UpdateInput{Content: &content})
	require.NoError(t, err)
	require.Len(t, f.vector.upserts, 1)
}

func TestListPagePaginationAndTenantScope(t *testing.T) {
	ctx := context.Background()
	f := newUpdateFixture(t)
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		ids = append(ids, f.store(t, "tenant_a", fmt.Sprintf("memory %d", i)).ID)
	}
	f.store(t, "tenant_b", "other tenant")

	first, err := f.svc.ListPage(ctx, "tenant_a", ListPageOptions{Limit: 2})
	require.NoError(t, err)
	require.Len(t, first.Items, 2)
	require.Equal(t, ids[4], first.Items[0].ID)
	require.NotNil(t, first.NextOffset)
	require.Equal(t, 2, *first.NextOffset)

	last, err := f.svc.ListPage(ctx, "tenant_a", ListPageOptions{Limit: 2, Offset: 4})
	require.NoError(t, err)
	require.Len(t, last.Items, 1)
	require.Equal(t, ids[0], last.Items[0].ID)
	require.Nil(t, last.NextOffset)

	exact, err := f.svc.ListPage(ctx, "tenant_a", ListPageOptions{Limit: 5})
	require.NoError(t, err)
	require.Len(t, exact.Items, 5)
	require.Nil(t, exact.NextOffset)

	_, err = f.svc.ListPage(ctx, "tenant_a", ListPageOptions{Offset: -1})
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	_, err = f.svc.ListPage(ctx, "tenant_missing", ListPageOptions{})
	require.ErrorIs(t, err, domain.ErrNotFound)
}
