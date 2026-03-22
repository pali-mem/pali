package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestDashboardTenantAndMemoryFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)

	form := url.Values{}
	form.Set("id", "tenant_dash")
	form.Set("name", "Tenant Dash")
	w := postDashboardForm(r, "/dashboard/tenants", form)
	require.Equal(t, http.StatusSeeOther, w.Code)

	w = performRequest(r, http.MethodGet, "/dashboard/tenants", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "tenant_dash")
	require.Contains(t, w.Body.String(), "Tenant Dash")

	form = url.Values{}
	form.Set("tenant_id", "tenant_dash")
	form.Set("content", "dashboard memory content")
	form.Set("tier", "semantic")
	form.Set("tags", "dash,test")
	w = postDashboardForm(r, "/dashboard/memories", form)
	require.Equal(t, http.StatusSeeOther, w.Code)

	w = performRequest(r, http.MethodGet, "/dashboard/memories?tenant_id=tenant_dash", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "dashboard memory content")

	searchBody := `{"tenant_id":"tenant_dash","query":"dashboard","top_k":5}`
	w = performRequest(r, http.MethodPost, "/v1/memory/search", searchBody, map[string]string{
		"Content-Type": "application/json",
	})
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Items)

	form = url.Values{}
	form.Set("tenant_id", "tenant_dash")
	w = postDashboardForm(r, "/dashboard/memories/"+resp.Items[0].ID+"/delete", form)
	require.Equal(t, http.StatusSeeOther, w.Code)

	w = performRequest(r, http.MethodGet, "/dashboard/stats", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "Total Tenants")

	w = performRequest(r, http.MethodGet, "/dashboard/analytics", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "In-memory operations view")

	w = performRequest(r, http.MethodGet, "/dashboard/analytics/data", "", nil)
	require.Equal(t, http.StatusOK, w.Code)
	var analytics struct {
		SearchCount int64 `json:"search_count"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &analytics))
	require.GreaterOrEqual(t, analytics.SearchCount, int64(1))
}

func postDashboardForm(r *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	return performRequest(r, http.MethodPost, path, form.Encode(), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	})
}

func performRequest(r *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// Ensure explicit empty body does not set text/plain automatically.
	if strings.TrimSpace(body) == "" {
		req.Body = http.NoBody
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
