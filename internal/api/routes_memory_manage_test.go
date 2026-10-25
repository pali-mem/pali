package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/config"
	"github.com/stretchr/testify/require"
)

type memoryBody struct {
	ID         string   `json:"id"`
	TenantID   string   `json:"tenant_id"`
	Content    string   `json:"content"`
	Tier       string   `json:"tier"`
	Kind       string   `json:"kind"`
	Tags       []string `json:"tags"`
	Importance float64  `json:"importance"`
}

type listBody struct {
	Items      []memoryBody `json:"items"`
	NextOffset *int         `json:"next_offset"`
}

func doJSON(t *testing.T, r http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Buffer
	if body != "" {
		reader = bytes.NewBufferString(body)
	} else {
		reader = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func seedTenant(t *testing.T, r http.Handler, id, token string) {
	t.Helper()
	w := doJSON(t, r, http.MethodPost, "/v1/tenants", fmt.Sprintf(`{"id":%q,"name":%q}`, id, id), token)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func seedMemory(t *testing.T, r http.Handler, tenantID, content, extra, token string) string {
	t.Helper()
	body := fmt.Sprintf(`{"tenant_id":%q,"content":%q%s}`, tenantID, content, extra)
	w := doJSON(t, r, http.MethodPost, "/v1/memory", body, token)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var out struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out.ID
}

func TestMemoryGetRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)
	seedTenant(t, r, "tenant_a", "")
	seedTenant(t, r, "tenant_b", "")
	id := seedMemory(t, r, "tenant_a", "user likes tea", `,"tier":"semantic","tags":["pref"]`, "")

	w := doJSON(t, r, http.MethodGet, "/v1/memory/"+id+"?tenant_id=tenant_a", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got memoryBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, id, got.ID)
	require.Equal(t, "tenant_a", got.TenantID)
	require.Equal(t, "user likes tea", got.Content)
	require.Equal(t, "semantic", got.Tier)
	require.Equal(t, []string{"pref"}, got.Tags)

	require.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodGet, "/v1/memory/mem_missing?tenant_id=tenant_a", "", "").Code)
	require.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodGet, "/v1/memory/"+id+"?tenant_id=tenant_b", "", "").Code)
	require.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodGet, "/v1/memory/"+id+"?tenant_id=tenant_missing", "", "").Code)
	require.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodGet, "/v1/memory/"+id, "", "").Code)

	// The static jobs route still wins over the :id parameter.
	require.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/v1/memory/jobs?tenant_id=tenant_a", "", "").Code)
}

func TestMemoryListRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)
	seedTenant(t, r, "tenant_a", "")
	seedTenant(t, r, "tenant_b", "")
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		extra := `,"tier":"episodic"`
		if i == 3 {
			extra = `,"tier":"semantic"`
		}
		ids = append(ids, seedMemory(t, r, "tenant_a", fmt.Sprintf("list item %d", i), extra, ""))
	}
	seedMemory(t, r, "tenant_b", "other tenant item", "", "")

	var page listBody
	w := doJSON(t, r, http.MethodGet, "/v1/memory?tenant_id=tenant_a&limit=2", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Items, 2)
	require.Equal(t, ids[4], page.Items[0].ID)
	require.Equal(t, ids[3], page.Items[1].ID)
	require.NotNil(t, page.NextOffset)
	require.Equal(t, 2, *page.NextOffset)

	page = listBody{}
	w = doJSON(t, r, http.MethodGet, "/v1/memory?tenant_id=tenant_a&limit=2&offset=4", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, ids[0], page.Items[0].ID)
	require.Nil(t, page.NextOffset)
	require.NotContains(t, w.Body.String(), "next_offset")

	page = listBody{}
	w = doJSON(t, r, http.MethodGet, "/v1/memory?tenant_id=tenant_a&tier=semantic", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, ids[3], page.Items[0].ID)

	page = listBody{}
	w = doJSON(t, r, http.MethodGet, "/v1/memory?tenant_id=tenant_a&kind=observation", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Empty(t, page.Items)
	require.Contains(t, w.Body.String(), `"items":[]`)

	// Default limit applies and a limit above the cap is clamped, not rejected.
	require.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/v1/memory?tenant_id=tenant_a&limit=1000", "", "").Code)

	for _, bad := range []string{
		"tenant_id=tenant_a&limit=0",
		"tenant_id=tenant_a&limit=abc",
		"tenant_id=tenant_a&offset=-1",
		"tenant_id=tenant_a&offset=x",
		"tenant_id=tenant_a&tier=bogus",
		"tenant_id=tenant_a&tier=auto",
		"tenant_id=tenant_a&kind=bogus",
		"limit=5",
	} {
		require.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodGet, "/v1/memory?"+bad, "", "").Code, bad)
	}
	require.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodGet, "/v1/memory?tenant_id=tenant_missing", "", "").Code)
}

func TestMemoryPatchRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)
	seedTenant(t, r, "tenant_a", "")
	seedTenant(t, r, "tenant_b", "")
	id := seedMemory(t, r, "tenant_a", "user likes zebrafish", `,"tier":"episodic","tags":["old"]`, "")
	path := "/v1/memory/" + id + "?tenant_id=tenant_a"

	w := doJSON(t, r, http.MethodPatch, path, `{"tags":["new"],"tier":"semantic","importance":0.7}`, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got memoryBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "user likes zebrafish", got.Content)
	require.Equal(t, []string{"new"}, got.Tags)
	require.Equal(t, "semantic", got.Tier)
	require.InDelta(t, 0.7, got.Importance, 0.0001)

	w = doJSON(t, r, http.MethodPatch, path, `{"content":"user likes narwhals"}`, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got = memoryBody{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "user likes narwhals", got.Content)
	require.Equal(t, []string{"new"}, got.Tags)
	require.Equal(t, "semantic", got.Tier)

	w = doJSON(t, r, http.MethodGet, path, "", "")
	require.Equal(t, http.StatusOK, w.Code)
	got = memoryBody{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Equal(t, "user likes narwhals", got.Content)

	w = doJSON(t, r, http.MethodPost, "/v1/memory/search", `{"tenant_id":"tenant_a","query":"narwhals","top_k":5,"disable_touch":true}`, "")
	require.Equal(t, http.StatusOK, w.Code)
	var search listBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &search))
	require.NotEmpty(t, search.Items)
	require.Equal(t, id, search.Items[0].ID)

	// Clearing tags with an empty array is a valid update.
	w = doJSON(t, r, http.MethodPatch, path, `{"tags":[]}`, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	got = memoryBody{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Empty(t, got.Tags)

	for name, body := range map[string]string{
		"empty object":     `{}`,
		"invalid json":     `{`,
		"blank content":    `{"content":"  "}`,
		"bad tier":         `{"tier":"bogus"}`,
		"empty tier":       `{"tier":""}`,
		"importance range": `{"importance":2}`,
	} {
		require.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodPatch, path, body, "").Code, name)
	}
	require.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodPatch, "/v1/memory/"+id, `{"tags":["x"]}`, "").Code)

	require.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodPatch, "/v1/memory/mem_missing?tenant_id=tenant_a", `{"tags":["x"]}`, "").Code)
	require.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodPatch, "/v1/memory/"+id+"?tenant_id=tenant_b", `{"tags":["x"]}`, "").Code)
	require.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodPatch, "/v1/memory/"+id+"?tenant_id=tenant_missing", `{"tags":["x"]}`, "").Code)

	// The other tenant's copy of the data is untouched by the rejected writes.
	w = doJSON(t, r, http.MethodGet, path, "", "")
	got = memoryBody{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Empty(t, got.Tags)
}

func TestTenantListAndRichStatsRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)

	w := doJSON(t, r, http.MethodGet, "/v1/tenants", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"items":[]}`, w.Body.String())

	seedTenant(t, r, "tenant_a", "")
	seedTenant(t, r, "tenant_b", "")
	seedMemory(t, r, "tenant_a", "first", `,"tier":"semantic"`, "")
	seedMemory(t, r, "tenant_a", "second", `,"tier":"episodic"`, "")
	seedMemory(t, r, "tenant_a", "third", `,"tier":"episodic","kind":"observation"`, "")

	w = doJSON(t, r, http.MethodGet, "/v1/tenants", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list struct {
		Items []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			MemoryCount int64  `json:"memory_count"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	counts := map[string]int64{}
	for _, item := range list.Items {
		counts[item.ID] = item.MemoryCount
	}
	require.Equal(t, map[string]int64{"tenant_a": 3, "tenant_b": 0}, counts)

	require.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/v1/tenants?limit=1", "", "").Code)
	require.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodGet, "/v1/tenants?limit=0", "", "").Code)

	// Recall counts only move when a search touches results.
	w = doJSON(t, r, http.MethodPost, "/v1/memory/search", `{"tenant_id":"tenant_a","query":"first","top_k":1}`, "")
	require.Equal(t, http.StatusOK, w.Code)

	w = doJSON(t, r, http.MethodGet, "/v1/tenants/tenant_a/stats", "", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var stats struct {
		TenantID       string           `json:"tenant_id"`
		MemoryCount    int64            `json:"memory_count"`
		ByTier         map[string]int64 `json:"by_tier"`
		ByKind         map[string]int64 `json:"by_kind"`
		TotalRecalls   int64            `json:"total_recalls"`
		NewestMemoryAt *time.Time       `json:"newest_memory_at"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &stats))
	require.Equal(t, "tenant_a", stats.TenantID)
	require.EqualValues(t, 3, stats.MemoryCount)
	require.Equal(t, map[string]int64{"semantic": 1, "episodic": 2}, stats.ByTier)
	require.Equal(t, map[string]int64{"raw_turn": 2, "observation": 1}, stats.ByKind)
	require.GreaterOrEqual(t, stats.TotalRecalls, int64(1))
	require.NotNil(t, stats.NewestMemoryAt)

	// An empty tenant still returns objects and a null newest time.
	w = doJSON(t, r, http.MethodGet, "/v1/tenants/tenant_b/stats", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"tenant_id":"tenant_b","memory_count":0,"by_tier":{},"by_kind":{},"total_recalls":0,"newest_memory_at":null}`, w.Body.String())
}

func newAuthTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := config.Defaults()
	cfg.Embedding.Provider = "mock"
	cfg.Database.SQLiteDSN = fmt.Sprintf("file:api_auth_manage_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	cfg.Auth.Enabled = true
	cfg.Auth.JWTSecret = "secret"
	cfg.Auth.Issuer = "pali"

	r, cleanup, err := NewRouter(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	return r
}

func TestNewV1RoutesEnforceTenantClaims(t *testing.T) {
	r := newAuthTestRouter(t)
	token1 := mustSignTenantToken(t, "secret", "pali", "tenant_1")
	token2 := mustSignTenantToken(t, "secret", "pali", "tenant_2")
	seedTenant(t, r, "tenant_1", token1)
	seedTenant(t, r, "tenant_2", token2)
	id1 := seedMemory(t, r, "tenant_1", "tenant one secret", "", token1)
	seedMemory(t, r, "tenant_2", "tenant two secret", "", token2)

	getPath := "/v1/memory/" + id1 + "?tenant_id=tenant_1"
	foreignGetPath := "/v1/memory/" + id1 + "?tenant_id=tenant_2"
	patchBody := `{"tags":["x"]}`

	// No token: every new route is rejected.
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, getPath, ""},
		{http.MethodGet, "/v1/memory?tenant_id=tenant_1", ""},
		{http.MethodPatch, getPath, patchBody},
		{http.MethodGet, "/v1/tenants", ""},
		{http.MethodGet, "/v1/tenants/tenant_1/stats", ""},
	} {
		require.Equal(t, http.StatusUnauthorized, doJSON(t, r, tc.method, tc.path, tc.body, "").Code, tc.method+" "+tc.path)
	}

	// Own tenant works.
	require.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, getPath, "", token1).Code)
	require.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/v1/memory?tenant_id=tenant_1", "", token1).Code)
	require.Equal(t, http.StatusOK, doJSON(t, r, http.MethodPatch, getPath, patchBody, token1).Code)
	require.Equal(t, http.StatusOK, doJSON(t, r, http.MethodGet, "/v1/tenants/tenant_1/stats", "", token1).Code)

	// Another tenant's claim is forbidden, including for a real memory ID.
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, getPath, ""},
		{http.MethodGet, "/v1/memory?tenant_id=tenant_1", ""},
		{http.MethodPatch, getPath, patchBody},
		{http.MethodGet, "/v1/tenants/tenant_1/stats", ""},
	} {
		require.Equal(t, http.StatusForbidden, doJSON(t, r, tc.method, tc.path, tc.body, token2).Code, tc.method+" "+tc.path)
	}

	// Own claim with a foreign memory ID resolves inside the claim tenant only.
	require.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodGet, foreignGetPath, "", token2).Code)
	require.Equal(t, http.StatusNotFound, doJSON(t, r, http.MethodPatch, foreignGetPath, patchBody, token2).Code)

	// A missing tenant_id is a bad request, not a fallback to the claim.
	require.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodGet, "/v1/memory/"+id1, "", token1).Code)
	require.Equal(t, http.StatusBadRequest, doJSON(t, r, http.MethodGet, "/v1/memory", "", token1).Code)

	// Tenant listing is limited to the caller's own tenant.
	w := doJSON(t, r, http.MethodGet, "/v1/tenants", "", token1)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list struct {
		Items []struct {
			ID          string `json:"id"`
			MemoryCount int64  `json:"memory_count"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "tenant_1", list.Items[0].ID)
	require.EqualValues(t, 1, list.Items[0].MemoryCount)

	// The foreign memory content never appears in tenant_1's list.
	w = doJSON(t, r, http.MethodGet, "/v1/memory?tenant_id=tenant_1", "", token1)
	require.NotContains(t, w.Body.String(), "tenant two secret")
}
