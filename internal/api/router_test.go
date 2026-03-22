package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/config"
	"github.com/stretchr/testify/require"
)

func TestRootRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Defaults()
	cfg.Embedding.Provider = "mock"
	cfg.Database.SQLiteDSN = fmt.Sprintf("file:router_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	r, cleanup, err := NewRouter(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, cleanup()) }()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t, "/dashboard", w.Header().Get("Location"))
}

func TestDashboardRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Defaults()
	cfg.Embedding.Provider = "mock"
	cfg.Database.SQLiteDSN = fmt.Sprintf("file:router_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	r, cleanup, err := NewRouter(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, cleanup()) }()

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t, "/dashboard/stats", w.Header().Get("Location"))

	req = httptest.NewRequest(http.MethodGet, "/dashboard/stats", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, strings.Contains(w.Body.String(), "Pali Dashboard"), "dashboard body: %q", w.Body.String())

	req = httptest.NewRequest(http.MethodGet, "/dashboard/config", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "Current configuration")

	req = httptest.NewRequest(http.MethodGet, "/dashboard/analytics", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "Live telemetry")

	req = httptest.NewRequest(http.MethodGet, "/dashboard/analytics/data", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Contains(t, payload, "active_requests")
	require.Contains(t, payload, "requests_per_minute")
}
