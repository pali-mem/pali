package tools

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corememory "github.com/vein05/pali/internal/core/memory"
	coretenant "github.com/vein05/pali/internal/core/tenant"
	"github.com/vein05/pali/internal/domain"
)

type Logger interface {
	Printf(format string, v ...any)
}

type ToolsetOptions struct {
	DefaultTenantID string
	AuthEnabled     bool
	Logger          Logger
}

type Toolset struct {
	memory *corememory.Service
	tenant *coretenant.Service

	defaultTenantID string
	authEnabled     bool
	logger          Logger

	mu             sync.RWMutex
	sessionTenants map[string]string
}

func NewToolset(memory *corememory.Service, tenant *coretenant.Service, opts ToolsetOptions) *Toolset {
	logger := opts.Logger
	if logger == nil {
		logger = log.Default()
	}

	return &Toolset{
		memory:          memory,
		tenant:          tenant,
		defaultTenantID: strings.TrimSpace(opts.DefaultTenantID),
		authEnabled:     opts.AuthEnabled,
		logger:          logger,
		sessionTenants:  map[string]string{},
	}
}

func (t *Toolset) Register(s *sdkmcp.Server) error {
	if t.memory == nil || t.tenant == nil {
		return fmt.Errorf("mcp toolset requires initialized services")
	}

	memoryStore := &sdkmcp.Tool{
		Name:        "memory_store",
		Description: "Write a durable memory item. Prefer this after learning user facts, plans, preferences, identity details, or corrections. Required: content (string). Optional: tenant_id (string), tier (auto|working|episodic|semantic), tags ([]string), source (string), created_by (auto|user|system). Tenant fallback order: tenant_id input -> JWT claim -> MCP session default -> config default_tenant_id.",
		Annotations: &sdkmcp.ToolAnnotations{
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
	}
	sdkmcp.AddTool(s, memoryStore, t.handleMemoryStore)

	memoryStorePreference := &sdkmcp.Tool{
		Name:        "memory_store_preference",
		Description: "Write a user preference in key/value form. Prefer this for stable defaults, style, likes, and dislikes. Required: key (string), value (string). Optional: tenant_id (string), tags ([]string). Tenant fallback order: tenant_id input -> JWT claim -> MCP session default -> config default_tenant_id.",
		Annotations: &sdkmcp.ToolAnnotations{
			DestructiveHint: boolPtr(false),
			OpenWorldHint:   boolPtr(false),
		},
	}
	sdkmcp.AddTool(s, memoryStorePreference, t.handleMemoryStorePreference)

	memorySearch := &sdkmcp.Tool{
		Name:        "memory_search",
		Description: "Primary recall tool. Call before answering user-specific or history-dependent requests, using the latest user message as query. Required: query (string). Optional: tenant_id (string), top_k (int, default 5), min_score (0..1), tiers ([working|episodic|semantic]), kinds ([raw_turn|observation|summary|event]). Tenant fallback order: tenant_id input -> JWT claim -> MCP session default -> config default_tenant_id.",
		Annotations: &sdkmcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: boolPtr(false),
		},
	}
	sdkmcp.AddTool(s, memorySearch, t.handleMemorySearch)

	memoryList := &sdkmcp.Tool{
		Name:        "memory_list",
		Description: "List recent memories for a tenant. Optional: tenant_id (string), limit (int). Tenant fallback order: tenant_id input -> JWT claim -> MCP session default -> config default_tenant_id.",
	}
	sdkmcp.AddTool(s, memoryList, t.handleMemoryList)

	memoryDelete := &sdkmcp.Tool{
		Name:        "memory_delete",
		Description: "Delete a memory by ID. Required fields: memory_id (string). Optional: tenant_id (string). Tenant fallback order: tenant_id input -> JWT claim -> MCP session default -> config default_tenant_id.",
	}
	sdkmcp.AddTool(s, memoryDelete, t.handleMemoryDelete)

	tenantCreate := &sdkmcp.Tool{
		Name:        "tenant_create",
		Description: "Create a tenant. Required fields: id (string), name (string).",
	}
	sdkmcp.AddTool(s, tenantCreate, t.handleTenantCreate)

	tenantList := &sdkmcp.Tool{
		Name:        "tenant_list",
		Description: "List all tenants. Optional: limit (int).",
	}
	sdkmcp.AddTool(s, tenantList, t.handleTenantList)

	tenantStats := &sdkmcp.Tool{
		Name:        "tenant_stats",
		Description: "Get memory count and stats for a tenant. Optional: tenant_id (string). Tenant fallback order: tenant_id input -> JWT claim -> MCP session default -> config default_tenant_id.",
	}
	sdkmcp.AddTool(s, tenantStats, t.handleTenantStats)

	tenantExists := &sdkmcp.Tool{
		Name:        "tenant_exists",
		Description: "Check if a tenant exists. Optional: tenant_id (string). Tenant fallback order: tenant_id input -> JWT claim -> MCP session default -> config default_tenant_id.",
	}
	sdkmcp.AddTool(s, tenantExists, t.handleTenantExists)

	healthCheck := &sdkmcp.Tool{
		Name:        "health_check",
		Description: "Basic MCP server health and time check.",
	}
	sdkmcp.AddTool(s, healthCheck, t.handleHealthCheck)

	capabilities := &sdkmcp.Tool{
		Name:        "pali_capabilities",
		Description: "List canonical Pali MCP tool names, tenant resolution behavior, and example tool calls.",
	}
	sdkmcp.AddTool(s, capabilities, t.handleCapabilitiesHelp)

	s.AddPrompt(&sdkmcp.Prompt{
		Name:        "memory_workflow",
		Description: "Instructs the agent to always use Pali memory tools: search before answering, store after learning.",
	}, func(_ context.Context, _ *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
		return &sdkmcp.GetPromptResult{
			Description: "Pali memory workflow instructions",
			Messages: []*sdkmcp.PromptMessage{
				{
					Role: "user",
					Content: &sdkmcp.TextContent{
						Text: "You have access to Pali, a long-term memory system. Follow this workflow for every interaction:\n\n" +
							"1. BEFORE answering: call memory_search with the user's message as the query (top_k 5). Use the results to inform your response.\n" +
							"2. WHILE responding: if the user shares facts, preferences, identity details, plans, or corrections, call memory_store or memory_store_preference to persist them.\n" +
							"3. Do NOT ask the user whether to remember something — just store it silently.",
					},
				},
			},
		}, nil
	})

	return nil
}

type MemoryStoreInput struct {
	TenantID  string   `json:"tenant_id,omitempty" jsonschema:"Tenant ID (optional if default resolution is configured)"`
	Content   string   `json:"content" jsonschema:"Memory content"`
	Tier      string   `json:"tier,omitempty" jsonschema:"Memory tier: auto|working|episodic|semantic"`
	Kind      string   `json:"kind,omitempty" jsonschema:"Memory kind: raw_turn|observation|summary|event"`
	Tags      []string `json:"tags,omitempty" jsonschema:"Tags"`
	Source    string   `json:"source,omitempty" jsonschema:"Origin/source label (message, api, import, etc.)"`
	CreatedBy string   `json:"created_by,omitempty" jsonschema:"Creator actor: auto|user|system"`
}

type MemoryStorePreferenceInput struct {
	TenantID string   `json:"tenant_id,omitempty" jsonschema:"Tenant ID (optional if default resolution is configured)"`
	Key      string   `json:"key" jsonschema:"Preference key"`
	Value    string   `json:"value" jsonschema:"Preference value"`
	Tags     []string `json:"tags,omitempty" jsonschema:"Additional tags"`
}

type MemorySearchInput struct {
	TenantID string   `json:"tenant_id,omitempty" jsonschema:"Tenant ID (optional if default resolution is configured)"`
	Query    string   `json:"query" jsonschema:"Search query"`
	TopK     int      `json:"top_k,omitempty" jsonschema:"Number of results"`
	MinScore float64  `json:"min_score,omitempty" jsonschema:"Minimum retrieval score, 0..1"`
	Tiers    []string `json:"tiers,omitempty" jsonschema:"Optional tier filter: working|episodic|semantic"`
	Kinds    []string `json:"kinds,omitempty" jsonschema:"Optional kind filter: raw_turn|observation|summary|event"`
}

type MemoryListInput struct {
	TenantID string `json:"tenant_id,omitempty" jsonschema:"Tenant ID (optional if default resolution is configured)"`
	Limit    int    `json:"limit,omitempty" jsonschema:"List limit"`
}

type MemoryDeleteInput struct {
	TenantID string `json:"tenant_id,omitempty" jsonschema:"Tenant ID (optional if default resolution is configured)"`
	MemoryID string `json:"memory_id" jsonschema:"Memory ID to delete"`
}

type TenantCreateInput struct {
	ID   string `json:"id" jsonschema:"Tenant ID"`
	Name string `json:"name" jsonschema:"Tenant display name"`
}

type TenantListInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"List limit"`
}

type TenantStatsInput struct {
	TenantID string `json:"tenant_id,omitempty" jsonschema:"Tenant ID (optional if default resolution is configured)"`
}

type TenantExistsInput struct {
	TenantID string `json:"tenant_id,omitempty" jsonschema:"Tenant ID (optional if default resolution is configured)"`
}

type MemoryItem struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	Content        string    `json:"content"`
	Tier           string    `json:"tier"`
	Kind           string    `json:"kind"`
	Tags           []string  `json:"tags"`
	Source         string    `json:"source"`
	CreatedBy      string    `json:"created_by"`
	Importance     float64   `json:"importance"`
	RecallCount    int       `json:"recall_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
	LastRecalledAt time.Time `json:"last_recalled_at"`
}

type TenantItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type MemoryStoreOutput struct {
	ID         string    `json:"id"`
	TenantID   string    `json:"tenant_id"`
	Tier       string    `json:"tier"`
	Source     string    `json:"source"`
	CreatedBy  string    `json:"created_by"`
	Importance float64   `json:"importance"`
	CreatedAt  time.Time `json:"created_at"`
}

type MemorySearchOutput struct {
	Items []MemoryItem `json:"items"`
}

type MemoryListOutput struct {
	Items []MemoryItem `json:"items"`
}

type MemoryDeleteOutput struct {
	Deleted bool `json:"deleted"`
}

type TenantCreateOutput struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type TenantListOutput struct {
	Items []TenantItem `json:"items"`
}

type TenantStatsOutput struct {
	TenantID    string `json:"tenant_id"`
	MemoryCount int64  `json:"memory_count"`
}

type TenantExistsOutput struct {
	TenantID string `json:"tenant_id"`
	Exists   bool   `json:"exists"`
}

type HealthCheckOutput struct {
	Status string    `json:"status"`
	Time   time.Time `json:"time"`
}

type CapabilitiesHelpOutput struct {
	CanonicalToolNames   []string          `json:"canonical_tool_names"`
	TenantResolutionPath []string          `json:"tenant_resolution_path"`
	ExampleCalls         []ToolCallExample `json:"example_calls"`
}

type ToolCallExample struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (t *Toolset) handleMemoryStore(ctx context.Context, req *sdkmcp.CallToolRequest, args MemoryStoreInput) (*sdkmcp.CallToolResult, MemoryStoreOutput, error) {
	tenantID, err := t.resolveTenant(ctx, req, args.TenantID)
	if err != nil {
		return nil, MemoryStoreOutput{}, err
	}

	tier, err := parseTier(args.Tier)
	if err != nil {
		return nil, MemoryStoreOutput{}, err
	}
	createdBy, err := parseCreatedBy(args.CreatedBy)
	if err != nil {
		return nil, MemoryStoreOutput{}, err
	}
	kind, err := parseMemoryKind(args.Kind)
	if err != nil {
		return nil, MemoryStoreOutput{}, err
	}
	stored, err := t.memory.Store(ctx, corememory.StoreInput{
		TenantID:  tenantID,
		Content:   args.Content,
		Tier:      tier,
		Kind:      kind,
		Tags:      args.Tags,
		Source:    args.Source,
		CreatedBy: createdBy,
	})
	if err != nil {
		return nil, MemoryStoreOutput{}, err
	}
	return toolOK("memory stored"), MemoryStoreOutput{
		ID:         stored.ID,
		TenantID:   stored.TenantID,
		Tier:       string(stored.Tier),
		Source:     stored.Source,
		CreatedBy:  string(stored.CreatedBy),
		Importance: stored.Importance,
		CreatedAt:  stored.CreatedAt,
	}, nil
}

func (t *Toolset) handleMemoryStorePreference(ctx context.Context, req *sdkmcp.CallToolRequest, args MemoryStorePreferenceInput) (*sdkmcp.CallToolResult, MemoryStoreOutput, error) {
	tenantID, err := t.resolveTenant(ctx, req, args.TenantID)
	if err != nil {
		return nil, MemoryStoreOutput{}, err
	}

	if strings.TrimSpace(args.Key) == "" || strings.TrimSpace(args.Value) == "" {
		return nil, MemoryStoreOutput{}, fmt.Errorf("%w: key and value are required", domain.ErrInvalidInput)
	}
	content := fmt.Sprintf("%s: %s", strings.TrimSpace(args.Key), strings.TrimSpace(args.Value))
	tags := append([]string{"preferences", strings.TrimSpace(args.Key)}, args.Tags...)

	stored, err := t.memory.Store(ctx, corememory.StoreInput{
		TenantID:  tenantID,
		Content:   content,
		Tier:      domain.MemoryTierSemantic,
		Tags:      dedupeTags(tags),
		Source:    "memory_store_preference",
		CreatedBy: domain.MemoryCreatedByAuto,
	})
	if err != nil {
		return nil, MemoryStoreOutput{}, err
	}
	return toolOK("preference stored"), MemoryStoreOutput{
		ID:         stored.ID,
		TenantID:   stored.TenantID,
		Tier:       string(stored.Tier),
		Source:     stored.Source,
		CreatedBy:  string(stored.CreatedBy),
		Importance: stored.Importance,
		CreatedAt:  stored.CreatedAt,
	}, nil
}

func (t *Toolset) handleMemorySearch(ctx context.Context, req *sdkmcp.CallToolRequest, args MemorySearchInput) (*sdkmcp.CallToolResult, MemorySearchOutput, error) {
	tenantID, err := t.resolveTenant(ctx, req, args.TenantID)
	if err != nil {
		return nil, MemorySearchOutput{}, err
	}

	if args.MinScore < 0 || args.MinScore > 1 {
		return toolError(domain.ErrInvalidInput), MemorySearchOutput{}, nil
	}
	searchTiers, err := parseSearchTiers(args.Tiers)
	if err != nil {
		return nil, MemorySearchOutput{}, err
	}
	searchKinds, err := parseSearchKinds(args.Kinds)
	if err != nil {
		return nil, MemorySearchOutput{}, err
	}
	items, err := t.memory.SearchWithFilters(ctx, tenantID, args.Query, args.TopK, corememory.SearchOptions{
		MinScore: args.MinScore,
		Tiers:    searchTiers,
		Kinds:    searchKinds,
	})
	if err != nil {
		return nil, MemorySearchOutput{}, err
	}
	return toolOK(fmt.Sprintf("%d memories found", len(items))), MemorySearchOutput{Items: mapMemoryItems(items)}, nil
}

func (t *Toolset) handleMemoryList(ctx context.Context, req *sdkmcp.CallToolRequest, args MemoryListInput) (*sdkmcp.CallToolResult, MemoryListOutput, error) {
	tenantID, err := t.resolveTenant(ctx, req, args.TenantID)
	if err != nil {
		return nil, MemoryListOutput{}, err
	}

	items, err := t.memory.List(ctx, tenantID, args.Limit)
	if err != nil {
		return nil, MemoryListOutput{}, err
	}
	return toolOK(fmt.Sprintf("%d memories listed", len(items))), MemoryListOutput{Items: mapMemoryItems(items)}, nil
}

func (t *Toolset) handleMemoryDelete(ctx context.Context, req *sdkmcp.CallToolRequest, args MemoryDeleteInput) (*sdkmcp.CallToolResult, MemoryDeleteOutput, error) {
	tenantID, err := t.resolveTenant(ctx, req, args.TenantID)
	if err != nil {
		return nil, MemoryDeleteOutput{}, err
	}

	if err := t.memory.Delete(ctx, tenantID, args.MemoryID); err != nil {
		return nil, MemoryDeleteOutput{}, err
	}
	return toolOK("memory deleted"), MemoryDeleteOutput{Deleted: true}, nil
}

func (t *Toolset) handleTenantCreate(ctx context.Context, req *sdkmcp.CallToolRequest, args TenantCreateInput) (*sdkmcp.CallToolResult, TenantCreateOutput, error) {
	created, err := t.tenant.Create(ctx, domain.Tenant{
		ID:   args.ID,
		Name: args.Name,
	})
	if err != nil {
		return nil, TenantCreateOutput{}, err
	}
	return toolOK("tenant created"), TenantCreateOutput{
		ID:        created.ID,
		Name:      created.Name,
		CreatedAt: created.CreatedAt,
	}, nil
}

func (t *Toolset) handleTenantList(ctx context.Context, req *sdkmcp.CallToolRequest, args TenantListInput) (*sdkmcp.CallToolResult, TenantListOutput, error) {
	tenants, err := t.tenant.List(ctx, args.Limit)
	if err != nil {
		return nil, TenantListOutput{}, err
	}
	out := make([]TenantItem, 0, len(tenants))
	for _, tenant := range tenants {
		out = append(out, TenantItem{ID: tenant.ID, Name: tenant.Name, CreatedAt: tenant.CreatedAt})
	}
	return toolOK(fmt.Sprintf("%d tenants listed", len(out))), TenantListOutput{Items: out}, nil
}

func (t *Toolset) handleTenantStats(ctx context.Context, req *sdkmcp.CallToolRequest, args TenantStatsInput) (*sdkmcp.CallToolResult, TenantStatsOutput, error) {
	tenantID, err := t.resolveTenant(ctx, req, args.TenantID)
	if err != nil {
		return nil, TenantStatsOutput{}, err
	}

	stats, err := t.tenant.Stats(ctx, tenantID)
	if err != nil {
		return nil, TenantStatsOutput{}, err
	}
	return toolOK("tenant stats loaded"), TenantStatsOutput{
		TenantID:    tenantID,
		MemoryCount: stats.MemoryCount,
	}, nil
}

func (t *Toolset) handleTenantExists(ctx context.Context, req *sdkmcp.CallToolRequest, args TenantExistsInput) (*sdkmcp.CallToolResult, TenantExistsOutput, error) {
	tenantID, err := t.resolveTenant(ctx, req, args.TenantID)
	if err != nil {
		return nil, TenantExistsOutput{}, err
	}

	exists, err := t.tenant.Exists(ctx, tenantID)
	if err != nil {
		return nil, TenantExistsOutput{}, err
	}
	return toolOK("tenant existence checked"), TenantExistsOutput{
		TenantID: tenantID,
		Exists:   exists,
	}, nil
}

func (t *Toolset) handleHealthCheck(ctx context.Context, req *sdkmcp.CallToolRequest, args struct{}) (*sdkmcp.CallToolResult, HealthCheckOutput, error) {
	return toolOK("ok"), HealthCheckOutput{
		Status: "ok",
		Time:   time.Now().UTC(),
	}, nil
}

func (t *Toolset) handleCapabilitiesHelp(ctx context.Context, req *sdkmcp.CallToolRequest, args struct{}) (*sdkmcp.CallToolResult, CapabilitiesHelpOutput, error) {
	return toolOK("capabilities loaded"), CapabilitiesHelpOutput{
		CanonicalToolNames: []string{
			"memory_store",
			"memory_store_preference",
			"memory_search",
			"memory_list",
			"memory_delete",
			"tenant_create",
			"tenant_list",
			"tenant_stats",
			"tenant_exists",
			"health_check",
			"pali_capabilities",
		},
		TenantResolutionPath: []string{
			"tenant_id in tool input",
			"JWT tenant claim (if auth enabled)",
			"MCP session default tenant",
			"default_tenant_id from config",
			"error when unresolved",
		},
		ExampleCalls: []ToolCallExample{
			{
				Name: "memory_store",
				Arguments: map[string]any{
					"content": "Jane graduates on May 22, 2027",
					"tier":    "semantic",
					"tags":    []string{"school", "important"},
				},
			},
			{
				Name: "memory_search",
				Arguments: map[string]any{
					"query": "When does Jane graduate?",
					"top_k": 5,
				},
			},
			{
				Name: "tenant_create",
				Arguments: map[string]any{
					"id":   "sugam_test",
					"name": "Sugam Test",
				},
			},
		},
	}, nil
}

func (t *Toolset) resolveTenant(ctx context.Context, req *sdkmcp.CallToolRequest, explicit string) (string, error) {
	if tenantID := strings.TrimSpace(explicit); tenantID != "" {
		t.rememberSessionTenant(req, tenantID)
		t.logTenantResolution(req, tenantID, "explicit")
		return tenantID, nil
	}

	if t.authEnabled {
		if tenantID := t.tenantFromJWT(ctx, req); tenantID != "" {
			t.rememberSessionTenant(req, tenantID)
			t.logTenantResolution(req, tenantID, "jwt")
			return tenantID, nil
		}
	}

	if tenantID := t.sessionTenant(req); tenantID != "" {
		t.logTenantResolution(req, tenantID, "session")
		return tenantID, nil
	}

	if t.defaultTenantID != "" {
		t.rememberSessionTenant(req, t.defaultTenantID)
		t.logTenantResolution(req, t.defaultTenantID, "config_default")
		return t.defaultTenantID, nil
	}

	// Single-tenant auto-detect: if exactly one tenant exists, use it automatically.
	// This makes pali work out-of-the-box for single-user setups without any config.
	if tenants, err := t.tenant.List(ctx, 2); err == nil && len(tenants) == 1 {
		tenantID := tenants[0].ID
		t.rememberSessionTenant(req, tenantID)
		t.logTenantResolution(req, tenantID, "single_tenant_auto")
		return tenantID, nil
	}

	return "", fmt.Errorf("%w: tenant_id is required (checked input, jwt, session default, default_tenant_id, and single-tenant auto-detect)", domain.ErrInvalidInput)
}

func (t *Toolset) tenantFromJWT(ctx context.Context, req *sdkmcp.CallToolRequest) string {
	if token := sdkauth.TokenInfoFromContext(ctx); token != nil {
		if tenantID := tenantFromTokenInfo(token); tenantID != "" {
			return tenantID
		}
	}
	if req != nil && req.Extra != nil && req.Extra.TokenInfo != nil {
		if tenantID := tenantFromTokenInfo(req.Extra.TokenInfo); tenantID != "" {
			return tenantID
		}
	}
	return ""
}

func tenantFromTokenInfo(info *sdkauth.TokenInfo) string {
	if info == nil || info.Extra == nil {
		return ""
	}
	for _, key := range []string{"tenant_id", "tenantId", "tenant"} {
		if tenantID := stringFromAny(info.Extra[key]); tenantID != "" {
			return tenantID
		}
	}
	return ""
}

func (t *Toolset) sessionTenant(req *sdkmcp.CallToolRequest) string {
	if req != nil && req.Session != nil {
		sessionID := strings.TrimSpace(req.Session.ID())
		if sessionID != "" {
			t.mu.RLock()
			tenantID := strings.TrimSpace(t.sessionTenants[sessionID])
			t.mu.RUnlock()
			if tenantID != "" {
				return tenantID
			}
		}

		initParams := req.Session.InitializeParams()
		if initParams != nil {
			if tenantID := tenantFromMeta(initParams.Meta); tenantID != "" {
				t.rememberSessionTenant(req, tenantID)
				return tenantID
			}
		}
	}

	if req != nil && req.Params != nil {
		if tenantID := tenantFromMeta(req.Params.Meta); tenantID != "" {
			t.rememberSessionTenant(req, tenantID)
			return tenantID
		}
	}

	return ""
}

func tenantFromMeta(meta sdkmcp.Meta) string {
	if meta == nil {
		return ""
	}
	for _, key := range []string{"tenant_id", "tenantId", "default_tenant_id", "defaultTenantId", "pali_tenant_id"} {
		if tenantID := stringFromAny(meta[key]); tenantID != "" {
			return tenantID
		}
	}
	return ""
}

func stringFromAny(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case fmt.Stringer:
		return strings.TrimSpace(x.String())
	default:
		return ""
	}
}

func (t *Toolset) rememberSessionTenant(req *sdkmcp.CallToolRequest, tenantID string) {
	if req == nil || req.Session == nil {
		return
	}

	sessionID := strings.TrimSpace(req.Session.ID())
	tenantID = strings.TrimSpace(tenantID)
	if sessionID == "" || tenantID == "" {
		return
	}

	t.mu.Lock()
	t.sessionTenants[sessionID] = tenantID
	t.mu.Unlock()
}

func (t *Toolset) logTenantResolution(req *sdkmcp.CallToolRequest, tenantID, source string) {
	if t.logger == nil {
		return
	}

	toolName := ""
	sessionID := ""
	if req != nil {
		if req.Params != nil {
			toolName = strings.TrimSpace(req.Params.Name)
		}
		if req.Session != nil {
			sessionID = strings.TrimSpace(req.Session.ID())
		}
	}

	t.logger.Printf("mcp tenant resolved source=%s tenant_id=%s tool=%s session_id=%s", source, tenantID, toolName, sessionID)
}

func mapMemoryItems(items []domain.Memory) []MemoryItem {
	out := make([]MemoryItem, 0, len(items))
	for _, m := range items {
		out = append(out, MemoryItem{
			ID:             m.ID,
			TenantID:       m.TenantID,
			Content:        m.Content,
			Tier:           string(m.Tier),
			Kind:           string(m.Kind),
			Tags:           m.Tags,
			Source:         m.Source,
			CreatedBy:      string(m.CreatedBy),
			Importance:     m.Importance,
			RecallCount:    m.RecallCount,
			CreatedAt:      m.CreatedAt,
			UpdatedAt:      m.UpdatedAt,
			LastAccessedAt: m.LastAccessedAt,
			LastRecalledAt: m.LastRecalledAt,
		})
	}
	return out
}

func dedupeTags(tags []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func boolPtr(v bool) *bool {
	return &v
}

func parseTier(raw string) (domain.MemoryTier, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(domain.MemoryTierAuto):
		return domain.MemoryTierAuto, nil
	case string(domain.MemoryTierWorking):
		return domain.MemoryTierWorking, nil
	case string(domain.MemoryTierEpisodic):
		return domain.MemoryTierEpisodic, nil
	case string(domain.MemoryTierSemantic):
		return domain.MemoryTierSemantic, nil
	default:
		return "", domain.ErrInvalidInput
	}
}

func parseSearchTiers(raw []string) ([]domain.MemoryTier, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[domain.MemoryTier]struct{}, len(raw))
	out := make([]domain.MemoryTier, 0, len(raw))
	for _, tierRaw := range raw {
		tier, err := parseTier(tierRaw)
		if err != nil {
			return nil, domain.ErrInvalidInput
		}
		if tier == domain.MemoryTierAuto {
			return nil, domain.ErrInvalidInput
		}
		if _, ok := seen[tier]; ok {
			continue
		}
		seen[tier] = struct{}{}
		out = append(out, tier)
	}
	return out, nil
}

func parseSearchKinds(raw []string) ([]domain.MemoryKind, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	seen := make(map[domain.MemoryKind]struct{}, len(raw))
	out := make([]domain.MemoryKind, 0, len(raw))
	for _, kindRaw := range raw {
		kind, err := parseMemoryKind(kindRaw)
		if err != nil {
			return nil, domain.ErrInvalidInput
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		out = append(out, kind)
	}
	return out, nil
}

func parseCreatedBy(raw string) (domain.MemoryCreatedBy, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(domain.MemoryCreatedByAuto):
		return domain.MemoryCreatedByAuto, nil
	case string(domain.MemoryCreatedByUser):
		return domain.MemoryCreatedByUser, nil
	case string(domain.MemoryCreatedBySystem):
		return domain.MemoryCreatedBySystem, nil
	default:
		return "", domain.ErrInvalidInput
	}
}

func parseMemoryKind(raw string) (domain.MemoryKind, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(domain.MemoryKindRawTurn):
		return domain.MemoryKindRawTurn, nil
	case string(domain.MemoryKindObservation):
		return domain.MemoryKindObservation, nil
	case string(domain.MemoryKindSummary):
		return domain.MemoryKindSummary, nil
	case string(domain.MemoryKindEvent):
		return domain.MemoryKindEvent, nil
	default:
		return "", domain.ErrInvalidInput
	}
}

func toolOK(text string) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
	}
}

func toolError(err error) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{
		IsError: true,
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: err.Error()}},
	}
}
