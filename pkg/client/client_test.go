package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	api "github.com/pali-mem/pali/internal/api"
	apiauth "github.com/pali-mem/pali/internal/auth"
	"github.com/pali-mem/pali/internal/config"
	"github.com/stretchr/testify/require"
)

func TestClientMemoryTenantFlow(t *testing.T) {
	c := newTestClient(t, false)

	ctx := context.Background()

	health, err := c.Health(ctx)
	require.NoError(t, err)
	require.Equal(t, "ok", health.Status)
	require.NotEmpty(t, health.Time)

	createdTenant, err := c.CreateTenant(ctx, CreateTenantRequest{
		ID:   "tenant_client_1",
		Name: "Tenant Client 1",
	})
	require.NoError(t, err)
	require.Equal(t, "tenant_client_1", createdTenant.ID)

	stats, err := c.TenantStats(ctx, "tenant_client_1")
	require.NoError(t, err)
	require.Equal(t, int64(0), stats.MemoryCount)

	stored, err := c.StoreMemory(ctx, StoreMemoryRequest{
		TenantID:  "tenant_client_1",
		Content:   "user likes coffee",
		Tags:      []string{"pref"},
		Tier:      "semantic",
		Source:    "client_test",
		CreatedBy: "user",
	})
	require.NoError(t, err)
	require.NotEmpty(t, stored.ID)

	batchStored, err := c.StoreMemoryBatch(ctx, StoreMemoryBatchRequest{
		Items: []StoreMemoryRequest{
			{
				TenantID: "tenant_client_1",
				Content:  "user likes tea",
				Tier:     "semantic",
				Source:   "client_test_batch",
			},
			{
				TenantID: "tenant_client_1",
				Content:  "user likes hiking",
				Tier:     "episodic",
				Source:   "client_test_batch",
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, batchStored.Items, 2)
	require.NotEmpty(t, batchStored.Items[0].ID)
	require.NotEmpty(t, batchStored.Items[1].ID)

	ingested, err := c.IngestMemory(ctx, StoreMemoryRequest{
		TenantID: "tenant_client_1",
		Content:  "user runs every morning",
		Tier:     "auto",
		Source:   "client_test_ingest",
	})
	require.NoError(t, err)
	require.NotEmpty(t, ingested.IngestID)
	require.Len(t, ingested.MemoryIDs, 1)
	require.NotEmpty(t, ingested.JobIDs)
	require.False(t, ingested.AcceptedAt.IsZero())

	ingestedBatch, err := c.IngestMemoryBatch(ctx, StoreMemoryBatchRequest{
		Items: []StoreMemoryRequest{
			{
				TenantID: "tenant_client_1",
				Content:  "user has a running club",
				Tier:     "semantic",
			},
			{
				TenantID: "tenant_client_1",
				Content:  "user prefers evening runs",
				Tier:     "episodic",
			},
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, ingestedBatch.IngestID)
	require.Len(t, ingestedBatch.MemoryIDs, 2)
	require.NotEmpty(t, ingestedBatch.JobIDs)
	require.False(t, ingestedBatch.AcceptedAt.IsZero())

	search, err := c.SearchMemory(ctx, SearchMemoryRequest{
		TenantID: "tenant_client_1",
		Query:    "coffee",
		TopK:     5,
	})
	require.NoError(t, err)
	foundStored := false
	for _, item := range search.Items {
		if item.ID == stored.ID {
			foundStored = true
			require.Equal(t, "client_test", item.Source)
			require.Equal(t, "user", item.CreatedBy)
		}
	}
	require.True(t, foundStored)

	jobs, err := c.ListPostprocessJobs(ctx, ListPostprocessJobsRequest{
		TenantID: "tenant_client_1",
		Limit:    20,
	})
	require.NoError(t, err)
	require.NotEmpty(t, jobs.Items)

	job, err := c.GetPostprocessJob(ctx, jobs.Items[0].ID)
	require.NoError(t, err)
	require.Equal(t, jobs.Items[0].ID, job.ID)
	require.Equal(t, "tenant_client_1", job.TenantID)

	err = c.DeleteMemory(ctx, "tenant_client_1", stored.ID)
	require.NoError(t, err)

	searchAfterDelete, err := c.SearchMemory(ctx, SearchMemoryRequest{
		TenantID: "tenant_client_1",
		Query:    "coffee",
		TopK:     5,
	})
	require.NoError(t, err)
	for _, item := range searchAfterDelete.Items {
		require.NotEqual(t, stored.ID, item.ID)
	}
}

func TestClientReturnsAPIError(t *testing.T) {
	c := newTestClient(t, false)

	ctx := context.Background()

	_, err := c.TenantStats(ctx, "missing_tenant")
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	require.Equal(t, "not found", apiErr.Message)

	_, err = c.CreateTenant(ctx, CreateTenantRequest{ID: "tenant_conflict", Name: "Tenant Conflict"})
	require.NoError(t, err)

	_, err = c.CreateTenant(ctx, CreateTenantRequest{ID: "tenant_conflict", Name: "Tenant Conflict"})
	require.Error(t, err)
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusConflict, apiErr.StatusCode)
	require.Equal(t, "conflict", apiErr.Message)
}

func TestClientBearerAuth(t *testing.T) {
	c := newTestClient(t, true)

	ctx := context.Background()

	_, err := c.CreateTenant(ctx, CreateTenantRequest{
		ID:   "tenant_auth_1",
		Name: "Tenant Auth 1",
	})
	require.Error(t, err)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)

	token, err := apiauth.GenerateTenantToken("secret", "pali", "tenant_auth_1", time.Hour)
	require.NoError(t, err)
	c.SetBearerToken(token)

	_, err = c.CreateTenant(ctx, CreateTenantRequest{
		ID:   "tenant_auth_1",
		Name: "Tenant Auth 1",
	})
	require.NoError(t, err)

	_, err = c.CreateTenant(ctx, CreateTenantRequest{
		ID:   "tenant_auth_2",
		Name: "Tenant Auth 2",
	})
	require.Error(t, err)
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	require.Equal(t, "tenant mismatch", apiErr.Message)
}

func TestNewClientValidation(t *testing.T) {
	_, err := NewClient("")
	require.Error(t, err)

	_, err = NewClient("localhost:8080")
	require.Error(t, err)

	_, err = NewClient("http://127.0.0.1:8080")
	require.NoError(t, err)
}

func newTestClient(t *testing.T, authEnabled bool) *Client {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg := config.Defaults()
	cfg.Embedding.Provider = "mock"
	cfg.Database.SQLiteDSN = fmt.Sprintf("file:pkg_client_%d?mode=memory&cache=shared", time.Now().UnixNano())
	cfg.Auth.Enabled = authEnabled
	cfg.Auth.JWTSecret = "secret"
	cfg.Auth.Issuer = "pali"

	r, cleanup, err := api.NewRouter(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, cleanup())
	})

	httpClient := &http.Client{
		Transport: localRoundTripper{handler: r},
	}
	c, err := NewClient("http://pali.test", WithHTTPClient(httpClient))
	require.NoError(t, err)
	return c
}

type localRoundTripper struct {
	handler http.Handler
}

func (rt localRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	rt.handler.ServeHTTP(recorder, req)
	return &http.Response{
		StatusCode: recorder.Code,
		Status:     fmt.Sprintf("%d %s", recorder.Code, http.StatusText(recorder.Code)),
		Header:     recorder.Result().Header.Clone(),
		Body:       io.NopCloser(recorder.Body),
		Request:    req,
	}, nil
}
