//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/pali-mem/pali/internal/domain"
	neo4jrepo "github.com/pali-mem/pali/internal/repository/neo4j"
	"github.com/stretchr/testify/require"
)

func TestLiveNeo4jDeleteByMemoryIDs(t *testing.T) {
	uri := os.Getenv("PALI_TEST_NEO4J_URI")
	if uri == "" {
		t.Skip("PALI_TEST_NEO4J_URI not set")
	}
	user := os.Getenv("PALI_TEST_NEO4J_USERNAME")
	if user == "" {
		user = "neo4j"
	}
	repo, err := neo4jrepo.NewEntityFactRepository(neo4jrepo.Options{
		URI:      uri,
		Username: user,
		Password: os.Getenv("PALI_TEST_NEO4J_PASSWORD"),
		Timeout:  10 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })

	ctx := context.Background()
	t1 := fmt.Sprintf("live_del_%d", time.Now().UnixNano())
	t2 := t1 + "_other"
	_, err = repo.StoreBatch(ctx, []domain.EntityFact{
		{TenantID: t1, Entity: "Carol", Relation: "lives_in", Value: "Oslo", MemoryID: "m1"},
		{TenantID: t1, Entity: "Carol", Relation: "works_at", Value: "Fjord", MemoryID: "m1"},
		{TenantID: t1, Entity: "Carol", Relation: "owns", Value: "Boat", MemoryID: "m2"},
		{TenantID: t2, Entity: "Carol", Relation: "lives_in", Value: "Lima", MemoryID: "m1"},
	})
	require.NoError(t, err)

	pathMemories := func(tenant string) map[string]bool {
		t.Helper()
		cands, err := repo.ListByEntityPaths(ctx, tenant, domain.EntityFactPathQuery{
			SeedEntities: []string{"carol"}, MaxHops: 2, Limit: 20,
		})
		require.NoError(t, err)
		out := map[string]bool{}
		for _, c := range cands {
			out[c.MemoryID] = true
		}
		return out
	}
	require.True(t, pathMemories(t1)["m1"], "m1 should be reachable before delete")

	require.NoError(t, repo.DeleteByMemoryIDs(ctx, t1, []string{"m1"}))

	gone, err := repo.ListByEntityRelations(ctx, t1, []string{"carol"}, []string{"lives_in", "works_at"}, 5)
	require.NoError(t, err)
	require.Empty(t, gone, "t1 facts for m1 must be deleted")
	kept, err := repo.ListByEntityRelation(ctx, t1, "carol", "owns", 5)
	require.NoError(t, err)
	require.Len(t, kept, 1, "t1 fact for m2 must remain")
	otherTenant, err := repo.ListByEntityRelation(ctx, t2, "carol", "lives_in", 5)
	require.NoError(t, err)
	require.Len(t, otherTenant, 1, "t2 fact with the same memory id must remain")

	require.False(t, pathMemories(t1)["m1"])
	require.True(t, pathMemories(t1)["m2"])
	require.True(t, pathMemories(t2)["m1"])
}
