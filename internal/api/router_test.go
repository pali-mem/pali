package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/config"
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
	require.Equal(t, "/dashboard/memories", w.Header().Get("Location"))

	req = httptest.NewRequest(http.MethodGet, "/dashboard/memories", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, strings.Contains(w.Body.String(), "Pali Dashboard"), "dashboard body: %q", w.Body.String())
}
