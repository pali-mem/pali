//go:build integration

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/api"
	"github.com/pali-mem/pali/internal/config"
	"github.com/pali-mem/pali/internal/domain"
	embedmock "github.com/pali-mem/pali/internal/embeddings/mock"
	neo4jrepo "github.com/pali-mem/pali/internal/repository/neo4j"
	pgvectorstore "github.com/pali-mem/pali/internal/vectorstore/pgvector"
	qdrantstore "github.com/pali-mem/pali/internal/vectorstore/qdrant"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

// Live backend tests. Each skips unless its env var points at a running
// service; `make test-backends` starts the containers and sets them.
//
//	PALI_TEST_QDRANT_URL     e.g. http://127.0.0.1:16333
//	PALI_TEST_PGVECTOR_DSN   e.g. postgres://pali:pali@127.0.0.1:15432/pali?sslmode=disable
//	PALI_TEST_NEO4J_URI      e.g. bolt://127.0.0.1:17687 (PALI_TEST_NEO4J_USERNAME / PALI_TEST_NEO4J_PASSWORD)

const (
	liveOldContent = "Jane keeps a pet zebrafish named Bubbles"
	liveNewContent = "Jane keeps a pet narwhal named Tusk"
)

func TestLiveQdrantMemoryLifecycle(t *testing.T) {
	baseURL := os.Getenv("PALI_TEST_QDRANT_URL")
	if baseURL == "" {
		t.Skip("PALI_TEST_QDRANT_URL not set")
	}
	collection := fmt.Sprintf("pali_live_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		req, err := http.NewRequest(http.MethodDelete, baseURL+"/collections/"+collection, nil)
		if err != nil {
			return
		}
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	})

	cfg := testutil.MustLoadProviderConfig(t, "mock")
	cfg.VectorBackend = "qdrant"
	cfg.Qdrant.BaseURL = baseURL
	cfg.Qdrant.Collection = collection
	cfg.Qdrant.TimeoutMS = 10000

	client, err := qdrantstore.NewClient(baseURL, "", collection, 10*time.Second)
	require.NoError(t, err)
	runLiveVectorLifecycle(t, cfg, qdrantstore.NewStore(client))
}

func TestLivePGVectorMemoryLifecycle(t *testing.T) {
	dsn := os.Getenv("PALI_TEST_PGVECTOR_DSN")
	if dsn == "" {
		t.Skip("PALI_TEST_PGVECTOR_DSN not set")
	}
	table := fmt.Sprintf("pali_live_%d", time.Now().UnixNano())

	cfg := testutil.MustLoadProviderConfig(t, "mock")
	cfg.VectorBackend = "pgvector"
	cfg.PGVector.DSN = dsn
	cfg.PGVector.Table = table
	cfg.PGVector.AutoMigrate = true

	store, err := pgvectorstore.NewStore(pgvectorstore.Options{DSN: dsn, Table: table, AutoMigrate: true})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = store.Close()
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return
		}
		defer func() { _ = db.Close() }()
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table)
	})
	runLiveVectorLifecycle(t, cfg, store)
}

// runLiveVectorLifecycle drives store -> search -> PATCH content -> search ->
// delete through the full router, and checks the vector backend directly via
// the supplied store handle (same backend, separate connection).
func runLiveVectorLifecycle(t *testing.T, cfg config.Config, direct domain.VectorStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx := context.Background()

	cfg.Database.SQLiteDSN = fmt.Sprintf("file:%s?cache=shared", filepath.Join(t.TempDir(), "live.sqlite"))
	router, cleanup, err := api.NewRouter(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })

	do := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	searchContents := func(query string) map[string]string {
		t.Helper()
		w := do(http.MethodPost, "/v1/memory/search", fmt.Sprintf(`{"tenant_id":"live","query":%q,"top_k":5}`, query))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out struct {
			Items []struct {
				ID      string `json:"id"`
				Content string `json:"content"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		contents := make(map[string]string, len(out.Items))
		for _, item := range out.Items {
			contents[item.ID] = item.Content
		}
		return contents
	}

	embedder := embedmock.NewEmbedder()
	similarity := func(text, id string) (float64, bool) {
		t.Helper()
		vec, err := embedder.Embed(ctx, text)
		require.NoError(t, err)
		candidates, err := direct.Search(ctx, "live", vec, 10)
		require.NoError(t, err)
		for _, c := range candidates {
			if c.MemoryID == id {
				return c.Similarity, true
			}
		}
		return 0, false
	}

	require.Equal(t, http.StatusCreated, do(http.MethodPost, "/v1/tenants", `{"id":"live","name":"Live"}`).Code)
	w := do(http.MethodPost, "/v1/memory", fmt.Sprintf(`{"tenant_id":"live","content":%q,"tier":"semantic"}`, liveOldContent))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.NotEmpty(t, created.ID)

	// Store: found by the lexical path and present in the vector backend.
	require.Equal(t, liveOldContent, searchContents("zebrafish")[created.ID])
	require.Eventually(t, func() bool {
		_, ok := similarity(liveOldContent, created.ID)
		return ok
	}, 10*time.Second, 200*time.Millisecond, "vector not stored")

	// PATCH content: FTS and vector are reindexed synchronously.
	w = do(http.MethodPatch, "/v1/memory/"+created.ID+"?tenant_id=live", fmt.Sprintf(`{"content":%q}`, liveNewContent))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// With one memory the vector path can still return it for any query, so
	// assert on the returned content: it must be the new text, never the old.
	require.Equal(t, liveNewContent, searchContents("narwhal")[created.ID])
	require.NotEqual(t, liveOldContent, searchContents("zebrafish")[created.ID])

	newSim, ok := similarity(liveNewContent, created.ID)
	require.True(t, ok, "vector missing after PATCH")
	oldSim, ok := similarity(liveOldContent, created.ID)
	require.True(t, ok)
	require.Greater(t, newSim, oldSim, "vector still reflects old content")
	require.InDelta(t, 1.0, newSim, 0.01, "vector should equal the embedding of the new content")

	// Delete: gone from API search and from the vector backend.
	require.Equal(t, http.StatusNoContent, do(http.MethodDelete, "/v1/memory/"+created.ID+"?tenant_id=live", "").Code)
	require.NotContains(t, searchContents("narwhal"), created.ID)
	_, ok = similarity(liveNewContent, created.ID)
	require.False(t, ok, "vector remains after delete")
}

func TestLiveNeo4jEntityFacts(t *testing.T) {
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
	tenant := fmt.Sprintf("live_%d", time.Now().UnixNano())
	now := time.Now().UTC()

	single, err := repo.Store(ctx, domain.EntityFact{
		TenantID: tenant, Entity: "Alice", Relation: "lives_in", Value: "Paris", MemoryID: "mem_1", CreatedAt: now.Add(-2 * time.Hour),
	})
	require.NoError(t, err)
	require.NotEmpty(t, single.ID)

	batch, err := repo.StoreBatch(ctx, []domain.EntityFact{
		{TenantID: tenant, Entity: "Alice", Relation: "lives_in", Value: "Berlin", MemoryID: "mem_2", CreatedAt: now.Add(-time.Hour)},
		{TenantID: tenant, Entity: "Alice", Relation: "works_at", Value: "Acme", MemoryID: "mem_3", CreatedAt: now},
		{TenantID: tenant, Entity: "Bob", Relation: "lives_in", Value: "Rome", MemoryID: "mem_4", CreatedAt: now},
	})
	require.NoError(t, err)
	require.Len(t, batch, 3)

	// Single lookup: newest first, per-pair limit.
	facts, err := repo.ListByEntityRelation(ctx, tenant, "alice", "lives_in", 10)
	require.NoError(t, err)
	require.Len(t, facts, 2)
	require.Equal(t, "Berlin", facts[0].Value)
	require.Equal(t, "Paris", facts[1].Value)

	limited, err := repo.ListByEntityRelation(ctx, tenant, "alice", "lives_in", 1)
	require.NoError(t, err)
	require.Len(t, limited, 1)

	// Batch lookup (the CALL { WITH pair ... } query), per-pair limit 1.
	got, err := repo.ListByEntityRelations(ctx, tenant, []string{"alice", "bob"}, []string{"lives_in", "works_at"}, 1)
	require.NoError(t, err)
	var pairs []string
	for _, f := range got {
		pairs = append(pairs, f.Entity+"/"+f.Relation+"="+f.Value)
	}
	require.Equal(t, []string{"alice/lives_in=Berlin", "alice/works_at=Acme", "bob/lives_in=Rome"}, pairs)

	// Tenant isolation.
	other, err := repo.ListByEntityRelations(ctx, tenant+"_other", []string{"alice"}, []string{"lives_in"}, 5)
	require.NoError(t, err)
	require.Empty(t, other)
}
