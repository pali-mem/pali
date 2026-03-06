//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"github.com/vein05/pali/internal/api"
	"github.com/vein05/pali/internal/config"
	corememory "github.com/vein05/pali/internal/core/memory"
	coretenant "github.com/vein05/pali/internal/core/tenant"
	embedmock "github.com/vein05/pali/internal/embeddings/mock"
	palimcp "github.com/vein05/pali/internal/mcp"
	sqliterepo "github.com/vein05/pali/internal/repository/sqlite"
	heuristicscorer "github.com/vein05/pali/internal/scorer/heuristic"
	"github.com/vein05/pali/internal/vectorstore/sqlitevec"
	paliclient "github.com/vein05/pali/pkg/client"
)

type e2eEnvironment struct {
	Client  *paliclient.Client
	Session *sdkmcp.ClientSession
	close   func()
}

func newE2EEnvironment(t *testing.T) *e2eEnvironment {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?cache=shared", t.TempDir()+"/e2e.sqlite")
	cfg := config.Defaults()
	cfg.Database.SQLiteDSN = dsn
	cfg.Embedding.Provider = "mock"

	gin.SetMode(gin.TestMode)
	router, apiCleanup, err := api.NewRouter(cfg)
	require.NoError(t, err)

	httpClient := &http.Client{
		Transport: localRoundTripper{handler: router},
		Timeout:   15 * time.Second,
	}
	apiClient, err := paliclient.NewClient("http://pali.e2e", paliclient.WithHTTPClient(httpClient))
	require.NoError(t, err)

	db, err := sqliterepo.Open(context.Background(), dsn)
	require.NoError(t, err)
	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	vectorStore := sqlitevec.NewStore(db)
	embedder := embedmock.NewEmbedder()
	scorer := heuristicscorer.NewScorer()
	memoryService := corememory.NewService(memoryRepo, tenantRepo, vectorStore, embedder, scorer)
	tenantService := coretenant.NewService(tenantRepo)

	mcpServer, err := palimcp.NewServer(palimcp.Services{
		Memory: memoryService,
		Tenant: tenantService,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	runErr := make(chan error, 1)
	go func() {
		runErr <- mcpServer.Run(ctx, serverTransport)
	}()

	mcpClient := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "pali-e2e-client",
		Version: "0.1.0",
	}, nil)
	session, err := mcpClient.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)

	return &e2eEnvironment{
		Client:  apiClient,
		Session: session,
		close: func() {
			_ = session.Close()
			cancel()
			select {
			case err := <-runErr:
				if err != nil && ctx.Err() == nil {
					require.NoError(t, err)
				}
			case <-time.After(200 * time.Millisecond):
			}
			require.NoError(t, db.Close())
			require.NoError(t, apiCleanup())
		},
	}
}

func (e *e2eEnvironment) Close() {
	if e != nil && e.close != nil {
		e.close()
	}
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
