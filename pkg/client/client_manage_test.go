package client

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	apiauth "github.com/pali-mem/pali/internal/auth"
	"github.com/stretchr/testify/require"
)

func TestClientGetListUpdateMemory(t *testing.T) {
	c := newTestClient(t, false)
	ctx := context.Background()

	_, err := c.CreateTenant(ctx, CreateTenantRequest{ID: "tenant_m", Name: "Tenant M"})
	require.NoError(t, err)
	_, err = c.CreateTenant(ctx, CreateTenantRequest{ID: "tenant_other", Name: "Tenant Other"})
	require.NoError(t, err)

	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		stored, err := c.StoreMemory(ctx, StoreMemoryRequest{
			TenantID: "tenant_m",
			Content:  fmt.Sprintf("client memory %d", i),
			Tier:     "episodic",
			Tags:     []string{"seed"},
		})
		require.NoError(t, err)
		ids = append(ids, stored.ID)
	}

	got, err := c.GetMemory(ctx, "tenant_m", ids[0])
	require.NoError(t, err)
	require.Equal(t, ids[0], got.ID)
	require.Equal(t, "client memory 0", got.Content)

	_, err = c.GetMemory(ctx, "tenant_other", ids[0])
	require.ErrorIs(t, err, ErrNotFound)
	_, err = c.GetMemory(ctx, "tenant_m", "mem_missing")
	require.ErrorIs(t, err, ErrNotFound)

	page, err := c.ListMemory(ctx, ListMemoryRequest{TenantID: "tenant_m", Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Equal(t, ids[2], page.Items[0].ID)
	require.NotNil(t, page.NextOffset)

	page, err = c.ListMemory(ctx, ListMemoryRequest{TenantID: "tenant_m", Limit: 2, Offset: *page.NextOffset})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, ids[0], page.Items[0].ID)
	require.Nil(t, page.NextOffset)

	page, err = c.ListMemory(ctx, ListMemoryRequest{TenantID: "tenant_m", Tiers: []string{"semantic"}})
	require.NoError(t, err)
	require.Empty(t, page.Items)

	_, err = c.ListMemory(ctx, ListMemoryRequest{TenantID: "tenant_m", Kinds: []string{"bogus"}})
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)

	content := "client memory rewritten"
	tags := []string{}
	tier := "semantic"
	importance := 0.75
	updated, err := c.UpdateMemory(ctx, "tenant_m", ids[0], UpdateMemoryRequest{
		Content:    &content,
		Tags:       &tags,
		Tier:       &tier,
		Importance: &importance,
	})
	require.NoError(t, err)
	require.Equal(t, content, updated.Content)
	require.Empty(t, updated.Tags)
	require.Equal(t, "semantic", updated.Tier)
	require.InDelta(t, 0.75, updated.Importance, 0.0001)

	refetched, err := c.GetMemory(ctx, "tenant_m", ids[0])
	require.NoError(t, err)
	require.Equal(t, content, refetched.Content)

	_, err = c.UpdateMemory(ctx, "tenant_m", ids[0], UpdateMemoryRequest{})
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	_, err = c.UpdateMemory(ctx, "tenant_other", ids[0], UpdateMemoryRequest{Content: &content})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestClientListTenantsAndStats(t *testing.T) {
	c := newTestClient(t, false)
	ctx := context.Background()

	empty, err := c.ListTenants(ctx, 0)
	require.NoError(t, err)
	require.Empty(t, empty.Items)

	_, err = c.CreateTenant(ctx, CreateTenantRequest{ID: "tenant_s", Name: "Tenant S"})
	require.NoError(t, err)
	_, err = c.StoreMemory(ctx, StoreMemoryRequest{TenantID: "tenant_s", Content: "stats memory", Tier: "semantic"})
	require.NoError(t, err)

	list, err := c.ListTenants(ctx, 10)
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, "tenant_s", list.Items[0].ID)
	require.EqualValues(t, 1, list.Items[0].MemoryCount)

	stats, err := c.TenantStats(ctx, "tenant_s")
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.MemoryCount)
	require.Equal(t, map[string]int64{"semantic": 1}, stats.ByTier)
	require.Equal(t, map[string]int64{"raw_turn": 1}, stats.ByKind)
	require.NotNil(t, stats.NewestMemoryAt)
}

func TestClientNewMethodsEnforceTenantClaims(t *testing.T) {
	c := newTestClient(t, true)
	ctx := context.Background()

	_, err := c.ListTenants(ctx, 0)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)

	token, err := apiauth.GenerateTenantToken("secret", "pali", "tenant_auth", time.Hour)
	require.NoError(t, err)
	c.SetBearerToken(token)
	_, err = c.CreateTenant(ctx, CreateTenantRequest{ID: "tenant_auth", Name: "Tenant Auth"})
	require.NoError(t, err)

	_, err = c.ListMemory(ctx, ListMemoryRequest{TenantID: "tenant_foreign"})
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	_, err = c.GetMemory(ctx, "tenant_foreign", "mem_x")
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	content := "x"
	_, err = c.UpdateMemory(ctx, "tenant_foreign", "mem_x", UpdateMemoryRequest{Content: &content})
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusForbidden, apiErr.StatusCode)

	list, err := c.ListTenants(ctx, 0)
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	require.Equal(t, "tenant_auth", list.Items[0].ID)
}
