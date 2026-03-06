//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	paliclient "github.com/vein05/pali/pkg/client"
)

func TestAPIMemoryFlow(t *testing.T) {
	env := newE2EEnvironment(t)
	t.Cleanup(env.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := env.Client.CreateTenant(ctx, paliclient.CreateTenantRequest{
		ID:   "tenant_api_a",
		Name: "Tenant API A",
	})
	require.NoError(t, err)
	_, err = env.Client.CreateTenant(ctx, paliclient.CreateTenantRequest{
		ID:   "tenant_api_b",
		Name: "Tenant API B",
	})
	require.NoError(t, err)

	stored, err := env.Client.StoreMemory(ctx, paliclient.StoreMemoryRequest{
		TenantID:  "tenant_api_a",
		Content:   "user prefers vim keybindings",
		Tier:      "semantic",
		Tags:      []string{"pref"},
		Source:    "e2e_api",
		CreatedBy: "user",
	})
	require.NoError(t, err)
	require.NotEmpty(t, stored.ID)

	searchA, err := env.Client.SearchMemory(ctx, paliclient.SearchMemoryRequest{
		TenantID: "tenant_api_a",
		Query:    "vim",
		TopK:     10,
	})
	require.NoError(t, err)
	require.Len(t, searchA.Items, 1)
	require.Equal(t, stored.ID, searchA.Items[0].ID)
	require.Equal(t, "e2e_api", searchA.Items[0].Source)
	require.Equal(t, "user", searchA.Items[0].CreatedBy)

	searchB, err := env.Client.SearchMemory(ctx, paliclient.SearchMemoryRequest{
		TenantID: "tenant_api_b",
		Query:    "vim",
		TopK:     10,
	})
	require.NoError(t, err)
	require.Len(t, searchB.Items, 0)

	require.NoError(t, env.Client.DeleteMemory(ctx, "tenant_api_a", stored.ID))

	searchAfterDelete, err := env.Client.SearchMemory(ctx, paliclient.SearchMemoryRequest{
		TenantID: "tenant_api_a",
		Query:    "vim",
		TopK:     10,
	})
	require.NoError(t, err)
	require.Len(t, searchAfterDelete.Items, 0)
}
