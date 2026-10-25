package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corememory "github.com/pali-mem/pali/internal/core/memory"
	coretenant "github.com/pali-mem/pali/internal/core/tenant"
	embedmock "github.com/pali-mem/pali/internal/embeddings/mock"
	"github.com/pali-mem/pali/internal/mcp/tools"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	heuristicscorer "github.com/pali-mem/pali/internal/scorer/heuristic"
	"github.com/pali-mem/pali/internal/vectorstore/sqlitevec"
	"github.com/stretchr/testify/require"
)

func TestServerRegistersExpectedTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, cleanup := newTestMCPServer(t)
	defer cleanup()

	session, stop := connectInMemorySession(ctx, t, server)
	defer stop()

	tools, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(tools.Tools), 13)

	names := make(map[string]struct{}, len(tools.Tools))
	for _, tool := range tools.Tools {
		names[tool.Name] = struct{}{}
	}
	expected := []string{
		"memory_store",
		"memory_store_preference",
		"memory_search",
		"memory_list",
		"memory_get",
		"memory_update",
		"memory_delete",
		"tenant_create",
		"tenant_list",
		"tenant_stats",
		"tenant_exists",
		"health_check",
		"pali_capabilities",
	}
	for _, name := range expected {
		_, ok := names[name]
		require.True(t, ok, "missing tool: %s", name)
	}
}

func TestServerExposesMemoryAutopilotGuidance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, cleanup := newTestMCPServer(t)
	defer cleanup()

	session, stop := connectInMemorySession(ctx, t, server)
	defer stop()

	initResult := session.InitializeResult()
	require.NotNil(t, initResult)
	require.Contains(t, initResult.Instructions, "memory_search")
	require.Contains(t, initResult.Instructions, "memory_store")

	prompts, err := session.ListPrompts(ctx, &sdkmcp.ListPromptsParams{})
	require.NoError(t, err)
	require.NotEmpty(t, prompts.Prompts)

	names := make(map[string]struct{}, len(prompts.Prompts))
	for _, prompt := range prompts.Prompts {
		names[prompt.Name] = struct{}{}
	}
	_, ok := names[promptMemoryAutopilotName]
	require.True(t, ok, "missing prompt: %s", promptMemoryAutopilotName)

	prompt, err := session.GetPrompt(ctx, &sdkmcp.GetPromptParams{
		Name:      promptMemoryAutopilotName,
		Arguments: map[string]string{},
	})
	require.NoError(t, err)
	require.NotEmpty(t, prompt.Messages)
	text, ok := prompt.Messages[0].Content.(*sdkmcp.TextContent)
	require.True(t, ok)
	require.Contains(t, text.Text, "memory_search")
	require.Contains(t, text.Text, "memory_store")
}

func TestServerToolFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, cleanup := newTestMCPServer(t)
	defer cleanup()

	session, stop := connectInMemorySession(ctx, t, server)
	defer stop()

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "pali_capabilities",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "tenant_create",
		Arguments: map[string]any{
			"id":   "tenant_mcp",
			"name": "Tenant MCP",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_store",
		Arguments: map[string]any{
			"tenant_id":  "tenant_mcp",
			"content":    "MCP wiring test memory",
			"tier":       "semantic",
			"tags":       []string{"mcp"},
			"source":     "mcp_test",
			"created_by": "user",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	storeOut := decodeStructured[tools.MemoryStoreOutput](t, res)
	require.Equal(t, "mcp_test", storeOut.Source)
	require.Equal(t, "user", storeOut.CreatedBy)

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_search",
		Arguments: map[string]any{
			"tenant_id": "tenant_mcp",
			"query":     "wiring",
			"top_k":     5,
			"tiers":     []string{"semantic"},
			"min_score": 0,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotNil(t, res.StructuredContent)
	searchOut := decodeStructured[tools.MemorySearchOutput](t, res)
	require.Len(t, searchOut.Items, 1)
	require.Equal(t, "mcp_test", searchOut.Items[0].Source)
	require.Equal(t, "user", searchOut.Items[0].CreatedBy)
}

func TestServerMemoryGetAndUpdate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, cleanup := newTestMCPServer(t)
	defer cleanup()

	session, stop := connectInMemorySession(ctx, t, server)
	defer stop()

	for _, id := range []string{"tenant_get", "tenant_get_other"} {
		res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
			Name:      "tenant_create",
			Arguments: map[string]any{"id": id, "name": id},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
	}

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_store",
		Arguments: map[string]any{
			"tenant_id": "tenant_get",
			"content":   "Jane graduates on May 22, 2027",
			"tier":      "episodic",
			"tags":      []string{"school"},
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	memoryID := decodeStructured[tools.MemoryStoreOutput](t, res).ID

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "memory_get",
		Arguments: map[string]any{"tenant_id": "tenant_get", "memory_id": memoryID},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	got := decodeStructured[tools.MemoryGetOutput](t, res)
	require.Equal(t, memoryID, got.Memory.ID)
	require.Equal(t, "Jane graduates on May 22, 2027", got.Memory.Content)

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "memory_get",
		Arguments: map[string]any{"tenant_id": "tenant_get_other", "memory_id": memoryID},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "memory_get",
		Arguments: map[string]any{"tenant_id": "tenant_get", "memory_id": "mem_missing"},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_update",
		Arguments: map[string]any{
			"tenant_id":  "tenant_get",
			"memory_id":  memoryID,
			"content":    "Jane graduates on May 29, 2027",
			"tags":       []string{},
			"tier":       "semantic",
			"importance": 0.6,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, toolResultText(res))
	updated := decodeStructured[tools.MemoryUpdateOutput](t, res)
	require.Equal(t, "Jane graduates on May 29, 2027", updated.Memory.Content)
	require.Empty(t, updated.Memory.Tags)
	require.Equal(t, "semantic", updated.Memory.Tier)
	require.InDelta(t, 0.6, updated.Memory.Importance, 0.0001)

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "memory_search",
		Arguments: map[string]any{"tenant_id": "tenant_get", "query": "May 29", "top_k": 3},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
	found := decodeStructured[tools.MemorySearchOutput](t, res)
	require.NotEmpty(t, found.Items)
	require.Equal(t, memoryID, found.Items[0].ID)

	for name, args := range map[string]map[string]any{
		"no fields":  {"tenant_id": "tenant_get", "memory_id": memoryID},
		"bad tier":   {"tenant_id": "tenant_get", "memory_id": memoryID, "tier": "bogus"},
		"wrong user": {"tenant_id": "tenant_get_other", "memory_id": memoryID, "content": "x"},
	} {
		res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "memory_update", Arguments: args})
		require.NoError(t, err, name)
		require.True(t, res.IsError, name)
	}

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "pali_capabilities", Arguments: map[string]any{}})
	require.NoError(t, err)
	caps := decodeStructured[tools.CapabilitiesHelpOutput](t, res)
	require.Contains(t, caps.CanonicalToolNames, "memory_get")
	require.Contains(t, caps.CanonicalToolNames, "memory_update")
}

func TestServerUsesConfigDefaultTenantWhenInputMissing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, cleanup := newTestMCPServerWithOptions(t, Options{DefaultTenantID: "tenant_default"})
	defer cleanup()

	session, stop := connectInMemorySession(ctx, t, server)
	defer stop()

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "tenant_create",
		Arguments: map[string]any{
			"id":   "tenant_default",
			"name": "Default Tenant",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_store",
		Arguments: map[string]any{
			"content": "tenant fallback memory",
			"tier":    "semantic",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestServerUsesSessionTenantAfterExplicitTenant(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, cleanup := newTestMCPServerWithOptions(t, Options{DefaultTenantID: "tenant_default"})
	defer cleanup()

	session, stop := connectInMemorySession(ctx, t, server)
	defer stop()

	for _, tenantID := range []string{"tenant_default", "tenant_override"} {
		res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
			Name: "tenant_create",
			Arguments: map[string]any{
				"id":   tenantID,
				"name": tenantID,
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError)
	}

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_store",
		Arguments: map[string]any{
			"tenant_id": "tenant_override",
			"content":   "session source memory",
			"tier":      "semantic",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	res, err = session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_search",
		Arguments: map[string]any{
			"query": "session source memory",
			"top_k": 5,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)
}

func TestServerErrorsWhenTenantCannotBeResolved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	server, cleanup := newTestMCPServerWithOptions(t, Options{})
	defer cleanup()

	session, stop := connectInMemorySession(ctx, t, server)
	defer stop()

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name: "memory_store",
		Arguments: map[string]any{
			"content": "no tenant source",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	require.Contains(t, toolResultText(res), "tenant_id is required")
}

func newTestMCPServer(t *testing.T) (*Server, func()) {
	return newTestMCPServerWithOptions(t, Options{})
}

func newTestMCPServerWithOptions(t *testing.T, opts Options) (*Server, func()) {
	t.Helper()
	ctx := context.Background()
	dsn := fmt.Sprintf("file:mcp_test_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := sqliterepo.Open(ctx, dsn)
	require.NoError(t, err)

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	vectorStore := sqlitevec.NewStore(db)
	embedder := embedmock.NewEmbedder()
	scorer := heuristicscorer.NewScorer()
	memoryService := corememory.NewService(memoryRepo, tenantRepo, vectorStore, embedder, scorer)
	tenantService := coretenant.NewService(tenantRepo)

	server, err := NewServer(Services{
		Memory: memoryService,
		Tenant: tenantService,
	}, opts)
	require.NoError(t, err)

	return server, func() { require.NoError(t, db.Close()) }
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

func connectInMemorySession(ctx context.Context, t *testing.T, server *Server) (*sdkmcp.ClientSession, func()) {
	t.Helper()
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()

	runErr := make(chan error, 1)
	go func() {
		runErr <- server.Run(ctx, serverTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "pali-test-client",
		Version: "0.1.0",
	}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	return session, func() {
		_ = session.Close()
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
		}
		select {
		case err := <-runErr:
			// Closing the client closes the in-memory pipe; whether the server
			// then reports io.ErrClosedPipe or nil depends on timing.
			if err != nil && ctx.Err() == nil && !errors.Is(err, io.ErrClosedPipe) {
				require.NoError(t, err)
			}
		default:
		}
	}
}
