//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func patchAuth(t *testing.T, router *gin.Engine, path string, token string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestMemoryGetListPatchIsolationStrictJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router, cleanup := newAuthenticatedIntegrationRouter(t)
	t.Cleanup(func() { require.NoError(t, cleanup()) })

	tokenA := mustGenerateTenantJWT(t, "tenant_a", time.Hour)
	tokenB := mustGenerateTenantJWT(t, "tenant_b", time.Hour)
	require.Equal(t, http.StatusCreated, postJSONAuth(t, router, "/v1/tenants", tokenA, `{"id":"tenant_a","name":"Tenant A"}`).Code)
	require.Equal(t, http.StatusCreated, postJSONAuth(t, router, "/v1/tenants", tokenB, `{"id":"tenant_b","name":"Tenant B"}`).Code)

	storeA := postJSONAuth(t, router, "/v1/memory", tokenA, `{"tenant_id":"tenant_a","content":"tenant a zebrafish note","tier":"semantic"}`)
	require.Equal(t, http.StatusCreated, storeA.Code)
	var createdA struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(storeA.Body.Bytes(), &createdA))
	require.Equal(t, http.StatusCreated, postJSONAuth(t, router, "/v1/memory", tokenB, `{"tenant_id":"tenant_b","content":"tenant b secret note","tier":"semantic"}`).Code)

	memA := "/v1/memory/" + createdA.ID
	require.Equal(t, http.StatusOK, getAuth(t, router, memA+"?tenant_id=tenant_a", tokenA).Code)
	require.Equal(t, http.StatusForbidden, getAuth(t, router, memA+"?tenant_id=tenant_a", tokenB).Code)
	require.Equal(t, http.StatusNotFound, getAuth(t, router, memA+"?tenant_id=tenant_b", tokenB).Code)

	require.Equal(t, http.StatusForbidden, patchAuth(t, router, memA+"?tenant_id=tenant_a", tokenB, `{"content":"hijack"}`).Code)
	require.Equal(t, http.StatusNotFound, patchAuth(t, router, memA+"?tenant_id=tenant_b", tokenB, `{"content":"hijack"}`).Code)

	patched := patchAuth(t, router, memA+"?tenant_id=tenant_a", tokenA, `{"content":"tenant a narwhal note","tags":["edited"]}`)
	require.Equal(t, http.StatusOK, patched.Code, patched.Body.String())

	search := postJSONAuth(t, router, "/v1/memory/search", tokenA, `{"tenant_id":"tenant_a","query":"narwhal","top_k":5}`)
	require.Equal(t, http.StatusOK, search.Code)
	var found struct {
		Items []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(search.Body.Bytes(), &found))
	require.NotEmpty(t, found.Items)
	require.Equal(t, createdA.ID, found.Items[0].ID)
	require.Equal(t, "tenant a narwhal note", found.Items[0].Content)

	require.Equal(t, http.StatusForbidden, getAuth(t, router, "/v1/memory?tenant_id=tenant_a", tokenB).Code)
	list := getAuth(t, router, "/v1/memory?tenant_id=tenant_b", tokenB)
	require.Equal(t, http.StatusOK, list.Code)
	require.NotContains(t, list.Body.String(), "narwhal")
	require.NotContains(t, list.Body.String(), createdA.ID)

	tenants := getAuth(t, router, "/v1/tenants", tokenA)
	require.Equal(t, http.StatusOK, tenants.Code)
	require.NotContains(t, tenants.Body.String(), "tenant_b")
	require.Equal(t, http.StatusUnauthorized, getAuth(t, router, "/v1/tenants", "not-a-token").Code)
}
