// Package mcp hosts the top-level MCP server wiring for Pali.
package mcp

import (
	"context"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corememory "github.com/pali-mem/pali/internal/core/memory"
	coretenant "github.com/pali-mem/pali/internal/core/tenant"
	"github.com/pali-mem/pali/internal/mcp/tools"
)

// Services bundles the core services needed to run the MCP server.
type Services struct {
	Memory *corememory.Service
	Tenant *coretenant.Service
}

// Logger is the minimal logging contract used by the MCP server.
type Logger interface {
	Printf(format string, v ...any)
}

// Options configures the MCP server.
type Options struct {
	DefaultTenantID string
	AuthEnabled     bool
	Logger          Logger
	Instructions    string
}

// Server wraps the SDK server instance used by Pali.
type Server struct {
	sdk *sdkmcp.Server
}

const defaultInstructions = "Use Pali as the default long-term memory layer. " +
	"Before answering user-specific or history-dependent questions, call memory_search using the latest user message as query (top_k 5 unless precision needs fewer). " +
	"When the user shares durable facts, preferences, identity details, plans, or corrections, write them with memory_store or memory_store_preference. " +
	"Use tenant fallback behavior and only ask for tenant_id if a tool call returns a tenant resolution error."

const (
	promptMemoryAutopilotName = "pali_memory_autopilot"
	promptMemoryAutopilotText = "Use Pali memory by default. " +
		"Before answering user-specific or history-dependent requests, call memory_search with the user's latest message. " +
		"After the user shares durable facts, preferences, identity details, plans, or corrections, call memory_store or memory_store_preference."
)

// NewServer constructs the MCP server and registers the default tools.
func NewServer(services Services, options ...Options) (*Server, error) {
	if services.Memory == nil || services.Tenant == nil {
		return nil, fmt.Errorf("mcp services are required")
	}
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}

	instructions := strings.TrimSpace(opts.Instructions)
	if instructions == "" {
		instructions = defaultInstructions
	}

	sdk := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "pali-mcp",
		Version: "0.1.0",
	}, &sdkmcp.ServerOptions{
		Instructions: instructions,
	})

	toolset := tools.NewToolset(services.Memory, services.Tenant, tools.ToolsetOptions{
		DefaultTenantID: opts.DefaultTenantID,
		AuthEnabled:     opts.AuthEnabled,
		Logger:          opts.Logger,
	})
	if err := toolset.Register(sdk); err != nil {
		return nil, err
	}
	addDefaultPrompts(sdk)

	return &Server{sdk: sdk}, nil
}

func addDefaultPrompts(s *sdkmcp.Server) {
	s.AddPrompt(&sdkmcp.Prompt{
		Name:        promptMemoryAutopilotName,
		Description: "Memory-first operating instructions for hosts and agents using Pali.",
	}, func(_ context.Context, _ *sdkmcp.GetPromptRequest) (*sdkmcp.GetPromptResult, error) {
		return &sdkmcp.GetPromptResult{
			Description: "Use this prompt to run Pali in memory-first mode.",
			Messages: []*sdkmcp.PromptMessage{
				{
					Role: "user",
					Content: &sdkmcp.TextContent{
						Text: promptMemoryAutopilotText,
					},
				},
			},
		}, nil
	})
}

// Run starts the MCP server over the provided transport.
func (s *Server) Run(ctx context.Context, transport sdkmcp.Transport) error {
	return s.sdk.Run(ctx, transport)
}

// RunStdio starts the MCP server over stdio.
func (s *Server) RunStdio(ctx context.Context) error {
	return s.Run(ctx, &sdkmcp.StdioTransport{})
}

// SDK returns the underlying SDK server.
func (s *Server) SDK() *sdkmcp.Server {
	return s.sdk
}
