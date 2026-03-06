package mcp

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	corememory "github.com/vein05/pali/internal/core/memory"
	coretenant "github.com/vein05/pali/internal/core/tenant"
	"github.com/vein05/pali/internal/mcp/tools"
)

type Services struct {
	Memory *corememory.Service
	Tenant *coretenant.Service
}

type Logger interface {
	Printf(format string, v ...any)
}

type Options struct {
	DefaultTenantID string
	AuthEnabled     bool
	Logger          Logger
}

type Server struct {
	sdk *sdkmcp.Server
}

func NewServer(services Services, options ...Options) (*Server, error) {
	if services.Memory == nil || services.Tenant == nil {
		return nil, fmt.Errorf("mcp services are required")
	}
	var opts Options
	if len(options) > 0 {
		opts = options[0]
	}

	sdk := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "pali-mcp",
		Version: "0.1.0",
	}, nil)

	toolset := tools.NewToolset(services.Memory, services.Tenant, tools.ToolsetOptions{
		DefaultTenantID: opts.DefaultTenantID,
		AuthEnabled:     opts.AuthEnabled,
		Logger:          opts.Logger,
	})
	if err := toolset.Register(sdk); err != nil {
		return nil, err
	}

	return &Server{sdk: sdk}, nil
}

func (s *Server) Run(ctx context.Context, transport sdkmcp.Transport) error {
	return s.sdk.Run(ctx, transport)
}

func (s *Server) RunStdio(ctx context.Context) error {
	return s.Run(ctx, &sdkmcp.StdioTransport{})
}

func (s *Server) SDK() *sdkmcp.Server {
	return s.sdk
}
