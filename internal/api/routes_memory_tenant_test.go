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
	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/config"
)

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	cfg := config.Defaults()
	cfg.Embedding.Provider = "mock"
	cfg.Database.SQLiteDSN = fmt.Sprintf("file:api_test_%d?mode=memory&cache=shared", time.Now().UnixNano())

	r, cleanup, err := NewRouter(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, cleanup()) })
	return r
}

func TestTenantCreateConflictAndStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)

	createBody := `{"id":"tenant_1","name":"Tenant One"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	req = httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant_1/stats", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var stats struct {
		TenantID    string `json:"tenant_id"`
		MemoryCount int64  `json:"memory_count"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &stats))
	require.Equal(t, "tenant_1", stats.TenantID)
	require.EqualValues(t, 0, stats.MemoryCount)
}

func TestMemoryCRUDValidationAndErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)

	createTenantReq := httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewBufferString(`{"id":"tenant_2","name":"Tenant Two"}`))
	createTenantReq.Header.Set("Content-Type", "application/json")
	createTenantW := httptest.NewRecorder()
	r.ServeHTTP(createTenantW, createTenantReq)
	require.Equal(t, http.StatusCreated, createTenantW.Code)

	createTenantReq = httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewBufferString(`{"id":"tenant_3","name":"Tenant Three"}`))
	createTenantReq.Header.Set("Content-Type", "application/json")
	createTenantW = httptest.NewRecorder()
	r.ServeHTTP(createTenantW, createTenantReq)
	require.Equal(t, http.StatusCreated, createTenantW.Code)

	invalidStoreReq := httptest.NewRequest(http.MethodPost, "/v1/memory", bytes.NewBufferString(`{"content":"missing tenant"}`))
	invalidStoreReq.Header.Set("Content-Type", "application/json")
	invalidStoreW := httptest.NewRecorder()
	r.ServeHTTP(invalidStoreW, invalidStoreReq)
	require.Equal(t, http.StatusBadRequest, invalidStoreW.Code)

	invalidBatchReq := httptest.NewRequest(http.MethodPost, "/v1/memory/batch", bytes.NewBufferString(`{"items":[]}`))
	invalidBatchReq.Header.Set("Content-Type", "application/json")
	invalidBatchW := httptest.NewRecorder()
	r.ServeHTTP(invalidBatchW, invalidBatchReq)
	require.Equal(t, http.StatusBadRequest, invalidBatchW.Code)

	batchReq := httptest.NewRequest(http.MethodPost, "/v1/memory/batch", bytes.NewBufferString(`{"items":[{"tenant_id":"tenant_2","content":"user likes tea","tier":"semantic","source":"api_batch"},{"tenant_id":"tenant_2","content":"user likes hiking","tier":"episodic","source":"api_batch"}]}`))
	batchReq.Header.Set("Content-Type", "application/json")
	batchW := httptest.NewRecorder()
	r.ServeHTTP(batchW, batchReq)
	require.Equal(t, http.StatusCreated, batchW.Code)
	var batchResp struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(batchW.Body.Bytes(), &batchResp))
	require.Len(t, batchResp.Items, 2)
	require.NotEmpty(t, batchResp.Items[0].ID)
	require.NotEmpty(t, batchResp.Items[1].ID)

	storeMissingTenantReq := httptest.NewRequest(http.MethodPost, "/v1/memory", bytes.NewBufferString(`{"tenant_id":"tenant_missing","content":"nope","tier":"semantic"}`))
	storeMissingTenantReq.Header.Set("Content-Type", "application/json")
	storeMissingTenantW := httptest.NewRecorder()
	r.ServeHTTP(storeMissingTenantW, storeMissingTenantReq)
	require.Equal(t, http.StatusNotFound, storeMissingTenantW.Code)

	invalidCreatedByReq := httptest.NewRequest(http.MethodPost, "/v1/memory", bytes.NewBufferString(`{"tenant_id":"tenant_2","content":"bad actor","tier":"semantic","created_by":"bot"}`))
	invalidCreatedByReq.Header.Set("Content-Type", "application/json")
	invalidCreatedByW := httptest.NewRecorder()
	r.ServeHTTP(invalidCreatedByW, invalidCreatedByReq)
	require.Equal(t, http.StatusBadRequest, invalidCreatedByW.Code)

	storeReq := httptest.NewRequest(http.MethodPost, "/v1/memory", bytes.NewBufferString(`{"tenant_id":"tenant_2","content":"user likes gin","tier":"semantic","tags":["pref"],"source":"api_test","created_by":"user"}`))
	storeReq.Header.Set("Content-Type", "application/json")
	storeW := httptest.NewRecorder()
	r.ServeHTTP(storeW, storeReq)
	require.Equal(t, http.StatusCreated, storeW.Code)

	var stored struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(storeW.Body.Bytes(), &stored))
	require.NotEmpty(t, stored.ID)

	invalidSearchReq := httptest.NewRequest(http.MethodPost, "/v1/memory/search", bytes.NewBufferString(`{"tenant_id":"tenant_2"}`))
	invalidSearchReq.Header.Set("Content-Type", "application/json")
	invalidSearchW := httptest.NewRecorder()
	r.ServeHTTP(invalidSearchW, invalidSearchReq)
	require.Equal(t, http.StatusBadRequest, invalidSearchW.Code)

	searchMissingTenantReq := httptest.NewRequest(http.MethodPost, "/v1/memory/search", bytes.NewBufferString(`{"tenant_id":"tenant_missing","query":"gin","top_k":5}`))
	searchMissingTenantReq.Header.Set("Content-Type", "application/json")
	searchMissingTenantW := httptest.NewRecorder()
	r.ServeHTTP(searchMissingTenantW, searchMissingTenantReq)
	require.Equal(t, http.StatusNotFound, searchMissingTenantW.Code)

	searchReq := httptest.NewRequest(http.MethodPost, "/v1/memory/search", bytes.NewBufferString(`{"tenant_id":"tenant_2","query":"gin","top_k":5}`))
	searchReq.Header.Set("Content-Type", "application/json")
	searchW := httptest.NewRecorder()
	r.ServeHTTP(searchW, searchReq)
	require.Equal(t, http.StatusOK, searchW.Code)

	var searchResp struct {
		Items []struct {
			ID        string `json:"id"`
			Source    string `json:"source"`
			CreatedBy string `json:"created_by"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(searchW.Body.Bytes(), &searchResp))
	foundStored := false
	for _, item := range searchResp.Items {
		if item.ID == stored.ID {
			foundStored = true
			require.Equal(t, "api_test", item.Source)
			require.Equal(t, "user", item.CreatedBy)
		}
	}
	require.True(t, foundStored)

	deleteWithoutTenantReq := httptest.NewRequest(http.MethodDelete, "/v1/memory/"+stored.ID, nil)
	deleteWithoutTenantW := httptest.NewRecorder()
	r.ServeHTTP(deleteWithoutTenantW, deleteWithoutTenantReq)
	require.Equal(t, http.StatusBadRequest, deleteWithoutTenantW.Code)

	deleteWithWrongTenantReq := httptest.NewRequest(http.MethodDelete, "/v1/memory/"+stored.ID+"?tenant_id=tenant_3", nil)
	deleteWithWrongTenantW := httptest.NewRecorder()
	r.ServeHTTP(deleteWithWrongTenantW, deleteWithWrongTenantReq)
	require.Equal(t, http.StatusNotFound, deleteWithWrongTenantW.Code)

	deleteWithMissingTenantReq := httptest.NewRequest(http.MethodDelete, "/v1/memory/"+stored.ID+"?tenant_id=tenant_missing", nil)
	deleteWithMissingTenantW := httptest.NewRecorder()
	r.ServeHTTP(deleteWithMissingTenantW, deleteWithMissingTenantReq)
	require.Equal(t, http.StatusNotFound, deleteWithMissingTenantW.Code)

	deleteNotFoundReq := httptest.NewRequest(http.MethodDelete, "/v1/memory/mem_missing?tenant_id=tenant_2", nil)
	deleteNotFoundW := httptest.NewRecorder()
	r.ServeHTTP(deleteNotFoundW, deleteNotFoundReq)
	require.Equal(t, http.StatusNotFound, deleteNotFoundW.Code)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/v1/memory/"+stored.ID+"?tenant_id=tenant_2", nil)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)
	require.Equal(t, http.StatusNoContent, deleteW.Code)
}

func TestTenantStatsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/tenants/tenant_missing/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestMemorySearchFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := newTestRouter(t)

	for _, tenant := range []string{"tenant_filters"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/tenants", bytes.NewBufferString(fmt.Sprintf(`{"id":"%s","name":"%s"}`, tenant, tenant)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
	}

	storeBodies := []string{
		`{"tenant_id":"tenant_filters","content":"user likes tea","tier":"semantic","tags":["pref"]}`,
		`{"tenant_id":"tenant_filters","content":"user likes hiking","tier":"episodic","tags":["event"]}`,
	}
	for _, body := range storeBodies {
		req := httptest.NewRequest(http.MethodPost, "/v1/memory", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
	}

	semanticSearchReq := httptest.NewRequest(http.MethodPost, "/v1/memory/search", bytes.NewBufferString(`{"tenant_id":"tenant_filters","query":"user likes","top_k":10,"tiers":["semantic"]}`))
	semanticSearchReq.Header.Set("Content-Type", "application/json")
	semanticSearchW := httptest.NewRecorder()
	r.ServeHTTP(semanticSearchW, semanticSearchReq)
	require.Equal(t, http.StatusOK, semanticSearchW.Code)

	var semanticResp struct {
		Items []struct {
			Tier string `json:"tier"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(semanticSearchW.Body.Bytes(), &semanticResp))
	require.Len(t, semanticResp.Items, 1)
	require.Equal(t, "semantic", semanticResp.Items[0].Tier)

	invalidTierReq := httptest.NewRequest(http.MethodPost, "/v1/memory/search", bytes.NewBufferString(`{"tenant_id":"tenant_filters","query":"user likes","top_k":10,"tiers":["invalid"]}`))
	invalidTierReq.Header.Set("Content-Type", "application/json")
	invalidTierW := httptest.NewRecorder()
	r.ServeHTTP(invalidTierW, invalidTierReq)
	require.Equal(t, http.StatusBadRequest, invalidTierW.Code)

	invalidMinScoreReq := httptest.NewRequest(http.MethodPost, "/v1/memory/search", bytes.NewBufferString(`{"tenant_id":"tenant_filters","query":"user likes","top_k":10,"min_score":1.5}`))
	invalidMinScoreReq.Header.Set("Content-Type", "application/json")
	invalidMinScoreW := httptest.NewRecorder()
	r.ServeHTTP(invalidMinScoreW, invalidMinScoreReq)
	require.Equal(t, http.StatusBadRequest, invalidMinScoreW.Code)
}
