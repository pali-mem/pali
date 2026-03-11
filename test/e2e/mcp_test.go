//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/pali-mem/pali/internal/mcp/tools"
	paliclient "github.com/pali-mem/pali/pkg/client"
)

func TestMCPCrossSurfaceFlow(t *testing.T) {
	env := newE2EEnvironment(t)
	t.Cleanup(env.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := env.Client.CreateTenant(ctx, paliclient.CreateTenantRequest{
		ID:   "tenant_mcp_cross",
		Name: "Tenant MCP Cross",
	})
	require.NoError(t, err)

	storeRes, err := env.Session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_store",
		Arguments: map[string]any{
			"tenant_id":  "tenant_mcp_cross",
			"content":    "cross surface memory",
			"tier":       "semantic",
			"tags":       []string{"cross", "mcp"},
			"source":     "e2e_mcp",
			"created_by": "system",
		},
	})
	require.NoError(t, err)
	require.False(t, storeRes.IsError, toolResultText(storeRes))

	storeOut := decodeStructured[tools.MemoryStoreOutput](t, storeRes)
	require.Equal(t, "tenant_mcp_cross", storeOut.TenantID)
	require.NotEmpty(t, storeOut.ID)
	require.Equal(t, "e2e_mcp", storeOut.Source)
	require.Equal(t, "system", storeOut.CreatedBy)

	apiSearch, err := env.Client.SearchMemory(ctx, paliclient.SearchMemoryRequest{
		TenantID: "tenant_mcp_cross",
		Query:    "cross surface",
		TopK:     5,
	})
	require.NoError(t, err)
	require.Len(t, apiSearch.Items, 1)
	require.Equal(t, storeOut.ID, apiSearch.Items[0].ID)
	require.Equal(t, "e2e_mcp", apiSearch.Items[0].Source)
	require.Equal(t, "system", apiSearch.Items[0].CreatedBy)

	require.NoError(t, env.Client.DeleteMemory(ctx, "tenant_mcp_cross", storeOut.ID))

	searchRes, err := env.Session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_search",
		Arguments: map[string]any{
			"tenant_id": "tenant_mcp_cross",
			"query":     "cross surface",
			"top_k":     5,
		},
	})
	require.NoError(t, err)
	require.False(t, searchRes.IsError, toolResultText(searchRes))

	searchOut := decodeStructured[tools.MemorySearchOutput](t, searchRes)
	require.Len(t, searchOut.Items, 0)
}

func decodeStructured[T any](t *testing.T, res *sdkmcp.CallToolResult) T {
	t.Helper()
	var out T
	require.NotNil(t, res)
	require.NotNil(t, res.StructuredContent)
	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func toolResultText(res *sdkmcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	for _, item := range res.Content {
		text, ok := item.(*sdkmcp.TextContent)
		if ok {
			return text.Text
		}
	}
	return ""
}
