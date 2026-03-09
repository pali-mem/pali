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

func TestTenantStatsLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := testutil.MustLoadProviderConfig(t, "mock")
	dbPath := filepath.Join(t.TempDir(), "tenant_stats.sqlite")
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
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	require.Equal(t, http.StatusCreated, postJSON("/v1/tenants", `{"id":"tenant_stats","name":"Tenant Stats"}`).Code)

	statsBefore := get("/v1/tenants/tenant_stats/stats")
	require.Equal(t, http.StatusOK, statsBefore.Code)
	var before struct {
		TenantID    string `json:"tenant_id"`
		MemoryCount int    `json:"memory_count"`
	}
	require.NoError(t, json.Unmarshal(statsBefore.Body.Bytes(), &before))
	require.Equal(t, "tenant_stats", before.TenantID)
	require.Equal(t, 0, before.MemoryCount)

	require.Equal(
		t,
		http.StatusCreated,
		postJSON("/v1/memory", `{"tenant_id":"tenant_stats","content":"tenant stats integration memory","tier":"semantic"}`).Code,
	)

	statsAfter := get("/v1/tenants/tenant_stats/stats")
	require.Equal(t, http.StatusOK, statsAfter.Code)
	var after struct {
		TenantID    string `json:"tenant_id"`
		MemoryCount int    `json:"memory_count"`
	}
	require.NoError(t, json.Unmarshal(statsAfter.Body.Bytes(), &after))
	require.Equal(t, "tenant_stats", after.TenantID)
	require.Equal(t, 1, after.MemoryCount)

	require.Equal(t, http.StatusNotFound, get("/v1/tenants/does_not_exist/stats").Code)
}
