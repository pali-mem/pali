package memory

import (
	"context"
	"testing"

	"github.com/pali-mem/pali/internal/domain"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	sqlitevec "github.com/pali-mem/pali/internal/vectorstore/sqlitevec"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

// contentParserStub returns the facts registered for an exact turn text.
type contentParserStub map[string][]ParsedFact

func (p contentParserStub) Parse(_ context.Context, content string, _ int) ([]ParsedFact, error) {
	return append([]ParsedFact{}, p[content]...), nil
}

type rederiveFixture struct {
	svc        *Service
	memRepo    *sqliterepo.MemoryRepository
	entityRepo *sqliterepo.EntityFactRepository
}

func newRederiveFixture(t *testing.T, opts ...ServiceOption) rederiveFixture {
	t.Helper()
	ctx := context.Background()
	db, err := sqliterepo.Open(ctx, testutil.InMemoryDBDSN())
	require.NoError(t, err)
	t.Cleanup(func() { sqliterepo.CloseDBForTest(db) })

	tenantRepo := sqliterepo.NewTenantRepository(db)
	_, err = tenantRepo.Create(ctx, domain.Tenant{ID: "tenant_a", Name: "tenant_a"})
	require.NoError(t, err)
	memRepo := sqliterepo.NewMemoryRepository(db)
	entityRepo := sqliterepo.NewEntityFactRepository(db)
	all := append([]ServiceOption{WithEntityFactRepository(entityRepo)}, opts...)
	return rederiveFixture{
		svc:        NewService(memRepo, tenantRepo, sqlitevec.NewStore(db), &textEmbedder{}, scorerStub{}, all...),
		memRepo:    memRepo,
		entityRepo: entityRepo,
	}
}

func (f rederiveFixture) derivedContents(t *testing.T) []string {
	t.Helper()
	page, err := f.memRepo.ListPage(context.Background(), "tenant_a", domain.MemoryListFilter{
		Limit: 100,
		Kinds: []domain.MemoryKind{domain.MemoryKindObservation, domain.MemoryKindEvent},
	})
	require.NoError(t, err)
	out := make([]string, 0, len(page))
	for _, m := range page {
		out = append(out, m.Content)
	}
	return out
}

func (f rederiveFixture) factValues(t *testing.T, entity, relation string) []string {
	t.Helper()
	facts, err := f.entityRepo.ListByEntityRelation(context.Background(), "tenant_a", entity, relation, 10)
	require.NoError(t, err)
	out := make([]string, 0, len(facts))
	for _, fact := range facts {
		out = append(out, fact.Value)
	}
	return out
}

func TestUpdateContentRederivesParsedFacts(t *testing.T) {
	const (
		oldTurn = "Alice: I moved to Paris last year."
		newTurn = "Alice: I moved to Berlin last year."
	)
	parser := contentParserStub{
		oldTurn: {{Content: "Alice lives in Paris.", Kind: domain.MemoryKindObservation, Entity: "Alice", Relation: "place", Value: "Paris"}},
		newTurn: {{Content: "Alice lives in Berlin.", Kind: domain.MemoryKindObservation, Entity: "Alice", Relation: "place", Value: "Berlin"}},
	}
	f := newRederiveFixture(t, ParserOptions{Enabled: true, StoreRawTurn: true, MaxFacts: 4}, WithInfoParser(parser))
	ctx := context.Background()

	raw, err := f.svc.Store(ctx, StoreInput{TenantID: "tenant_a", Content: oldTurn})
	require.NoError(t, err)
	require.Equal(t, []string{"Alice lives in Paris."}, f.derivedContents(t))
	require.Equal(t, []string{"Paris"}, f.factValues(t, "alice", "place"))

	content := newTurn
	_, err = f.svc.Update(ctx, "tenant_a", raw.ID, UpdateInput{Content: &content})
	require.NoError(t, err)

	require.Equal(t, []string{"Alice lives in Berlin."}, f.derivedContents(t))
	require.Equal(t, []string{"Berlin"}, f.factValues(t, "alice", "place"))
}

func TestUpdateContentKeepsDerivedMemoryReusedByNewText(t *testing.T) {
	const (
		oldTurn = "Bob: my dog is called Rex."
		newTurn = "Bob: my dog is called Rex, he is a beagle."
	)
	rex := ParsedFact{Content: "Bob's dog is named Rex.", Kind: domain.MemoryKindObservation, Entity: "Bob", Relation: "relationship", Value: "Rex"}
	parser := contentParserStub{
		oldTurn: {rex},
		newTurn: {rex, {Content: "Bob's dog is a beagle.", Kind: domain.MemoryKindObservation}},
	}
	f := newRederiveFixture(t, ParserOptions{Enabled: true, StoreRawTurn: true, MaxFacts: 4}, WithInfoParser(parser))
	ctx := context.Background()

	raw, err := f.svc.Store(ctx, StoreInput{TenantID: "tenant_a", Content: oldTurn})
	require.NoError(t, err)
	before, err := f.memRepo.ListBySourceTurnHash(ctx, "tenant_a", buildSourceTurnHash(oldTurn), 10)
	require.NoError(t, err)
	var rexID string
	for _, m := range before {
		if m.Kind == domain.MemoryKindObservation {
			rexID = m.ID
		}
	}
	require.NotEmpty(t, rexID)

	content := newTurn
	_, err = f.svc.Update(ctx, "tenant_a", raw.ID, UpdateInput{Content: &content})
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"Bob's dog is named Rex.", "Bob's dog is a beagle."}, f.derivedContents(t))
	require.Equal(t, []string{"Rex"}, f.factValues(t, "bob", "relationship"))
	after, err := f.memRepo.ListBySourceTurnHash(ctx, "tenant_a", buildSourceTurnHash(newTurn), 10)
	require.NoError(t, err)
	ids := make([]string, 0, len(after))
	for _, m := range after {
		ids = append(ids, m.ID)
	}
	require.Contains(t, ids, rexID, "reused derived memory moves to the edited turn")
}

func TestUpdateContentRederivesStructuredObservations(t *testing.T) {
	f := newRederiveFixture(t, StructuredMemoryOptions{Enabled: true, DualWriteObservations: true, MaxObservations: 2})
	ctx := context.Background()

	raw, err := f.svc.Store(ctx, StoreInput{TenantID: "tenant_a", Content: "I prefer green tea in the morning."})
	require.NoError(t, err)
	before := f.derivedContents(t)
	require.NotEmpty(t, before)

	content := "I prefer black coffee in the morning."
	_, err = f.svc.Update(ctx, "tenant_a", raw.ID, UpdateInput{Content: &content})
	require.NoError(t, err)

	after := f.derivedContents(t)
	require.NotEmpty(t, after)
	for _, text := range after {
		require.NotContains(t, text, "green tea")
	}
}

func TestUpdateDerivedMemoryContentDropsItsEntityFacts(t *testing.T) {
	const turn = "Carol: I work at Initech."
	parser := contentParserStub{
		turn: {{Content: "Carol works at Initech.", Kind: domain.MemoryKindObservation, Entity: "Carol", Relation: "role", Value: "Initech"}},
	}
	f := newRederiveFixture(t, ParserOptions{Enabled: true, StoreRawTurn: false, MaxFacts: 4}, WithInfoParser(parser))
	ctx := context.Background()

	observation, err := f.svc.Store(ctx, StoreInput{TenantID: "tenant_a", Content: turn})
	require.NoError(t, err)
	require.Equal(t, domain.MemoryKindObservation, observation.Kind)
	require.Equal(t, []string{"Initech"}, f.factValues(t, "carol", "role"))

	content := "Carol works at Globex."
	_, err = f.svc.Update(ctx, "tenant_a", observation.ID, UpdateInput{Content: &content})
	require.NoError(t, err)

	require.Empty(t, f.factValues(t, "carol", "role"))
	require.Equal(t, []string{"Carol works at Globex."}, f.derivedContents(t))
}

func TestUpdateTagsOnlyKeepsDerivedFacts(t *testing.T) {
	const turn = "Dan: I live in Oslo."
	parser := contentParserStub{
		turn: {{Content: "Dan lives in Oslo.", Kind: domain.MemoryKindObservation, Entity: "Dan", Relation: "place", Value: "Oslo"}},
	}
	f := newRederiveFixture(t, ParserOptions{Enabled: true, StoreRawTurn: true, MaxFacts: 4}, WithInfoParser(parser))
	ctx := context.Background()

	raw, err := f.svc.Store(ctx, StoreInput{TenantID: "tenant_a", Content: turn})
	require.NoError(t, err)

	tags := []string{"travel"}
	_, err = f.svc.Update(ctx, "tenant_a", raw.ID, UpdateInput{Tags: &tags})
	require.NoError(t, err)

	require.Equal(t, []string{"Dan lives in Oslo."}, f.derivedContents(t))
	require.Equal(t, []string{"Oslo"}, f.factValues(t, "dan", "place"))
}
