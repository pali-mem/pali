//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/api"
	"github.com/pali-mem/pali/test/testutil"
	"github.com/stretchr/testify/require"
)

func TestMemoryCRUDFlow_RealSQLite(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := testutil.MustLoadProviderConfig(t, "mock")
	dbPath := filepath.Join(t.TempDir(), "integration.sqlite")
	cfg.Database.SQLiteDSN = fmt.Sprintf("file:%s?cache=shared", dbPath)

	router, cleanup, err := api.NewRouter(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })

	postJSON := func(path string, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	del := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, http.StatusCreated, postJSON("/v1/tenants", `{"id":"tenant_a","name":"Tenant A"}`).Code)
	require.Equal(t, http.StatusCreated, postJSON("/v1/tenants", `{"id":"tenant_b","name":"Tenant B"}`).Code)

	storeResp := postJSON("/v1/memory", `{"tenant_id":"tenant_a","content":"user prefers vim","tier":"semantic","tags":["pref"]}`)
	require.Equal(t, http.StatusCreated, storeResp.Code)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(storeResp.Body.Bytes(), &created))
	require.NotEmpty(t, created.ID)

	searchA := postJSON("/v1/memory/search", `{"tenant_id":"tenant_a","query":"vim","top_k":10}`)
	require.Equal(t, http.StatusOK, searchA.Code)
	var resultA struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(searchA.Body.Bytes(), &resultA))
	require.Len(t, resultA.Items, 1)
	require.Equal(t, created.ID, resultA.Items[0].ID)

	searchB := postJSON("/v1/memory/search", `{"tenant_id":"tenant_b","query":"vim","top_k":10}`)
	require.Equal(t, http.StatusOK, searchB.Code)
	var resultB struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(searchB.Body.Bytes(), &resultB))
	require.Len(t, resultB.Items, 0)

	require.Equal(t, http.StatusNotFound, del("/v1/memory/"+created.ID+"?tenant_id=tenant_b").Code)
	require.Equal(t, http.StatusNoContent, del("/v1/memory/"+created.ID+"?tenant_id=tenant_a").Code)

	searchAAfterDelete := postJSON("/v1/memory/search", `{"tenant_id":"tenant_a","query":"vim","top_k":10}`)
	require.Equal(t, http.StatusOK, searchAAfterDelete.Code)
	var resultAfterDelete struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(searchAAfterDelete.Body.Bytes(), &resultAfterDelete))
	require.Len(t, resultAfterDelete.Items, 0)
}
