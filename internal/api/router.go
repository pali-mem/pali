package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/api/handlers"
	apimiddleware "github.com/pali-mem/pali/internal/api/middleware"
	apiauth "github.com/pali-mem/pali/internal/auth"
	"github.com/pali-mem/pali/internal/config"
	corememory "github.com/pali-mem/pali/internal/core/memory"
	coretenant "github.com/pali-mem/pali/internal/core/tenant"
	"github.com/pali-mem/pali/internal/dashboard"
	"github.com/pali-mem/pali/internal/embeddings"
	sqliterepo "github.com/pali-mem/pali/internal/repository/sqlite"
	"github.com/pali-mem/pali/internal/startup"
	"github.com/pali-mem/pali/internal/wiring"
)

func NewRouter(cfg config.Config) (*gin.Engine, func() error, error) {
	if gin.Mode() == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := sqliterepo.Open(context.Background(), cfg.Database.SQLiteDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open sqlite for router: %w", err)
	}
	stopPostprocess := func() {}
	closeEntityFactRepo := func() error { return nil }
	cleanup := func() error {
		stopPostprocess()
		if err := closeEntityFactRepo(); err != nil {
			_ = db.Close()
			return err
		}
		return db.Close()
	}

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	entityFactRepo, entityFactCleanup, err := wiring.BuildEntityFactRepository(cfg, db)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	closeEntityFactRepo = entityFactCleanup
	vectorStore, err := wiring.BuildVectorStore(cfg, db)
	if err != nil {
		_ = closeEntityFactRepo()
		_ = db.Close()
		return nil, nil, err
	}

	embedder, embedMeta, err := embeddings.BuildWithMetadata(cfg)
	if err != nil {
		_ = closeEntityFactRepo()
		_ = db.Close()
		return nil, nil, err
	}
	scorer, err := wiring.BuildImportanceScorer(cfg)
	if err != nil {
		_ = closeEntityFactRepo()
		_ = db.Close()
		return nil, nil, err
	}
	infoParser, err := wiring.BuildInfoParser(cfg)
	if err != nil {
		_ = closeEntityFactRepo()
		_ = db.Close()
		return nil, nil, err
	}
	decomposer, err := wiring.BuildMultiHopQueryDecomposer(cfg)
	if err != nil {
		_ = closeEntityFactRepo()
		_ = db.Close()
		return nil, nil, err
	}
	serviceOptions := wiring.BuildMemoryServiceOptions(cfg, infoParser, entityFactRepo, decomposer)
	memoryService := corememory.NewService(memoryRepo, tenantRepo, vectorStore, embedder, scorer, serviceOptions...)
	if cfg.Postprocess.Enabled {
		stop, err := memoryService.StartPostprocessWorkers(context.Background(), corememory.PostprocessWorkerOptions{
			Enabled:      cfg.Postprocess.Enabled,
			PollInterval: time.Duration(cfg.Postprocess.PollIntervalMS) * time.Millisecond,
			BatchSize:    cfg.Postprocess.BatchSize,
			WorkerCount:  cfg.Postprocess.WorkerCount,
			Lease:        time.Duration(cfg.Postprocess.LeaseMS) * time.Millisecond,
			MaxAttempts:  cfg.Postprocess.MaxAttempts,
			RetryBase:    time.Duration(cfg.Postprocess.RetryBaseMS) * time.Millisecond,
			RetryMax:     time.Duration(cfg.Postprocess.RetryMaxMS) * time.Millisecond,
		})
		if err != nil {
			_ = closeEntityFactRepo()
			_ = db.Close()
			return nil, nil, fmt.Errorf("start postprocess workers: %w", err)
		}
		stopPostprocess = stop
	}
	tenantService := coretenant.NewService(tenantRepo)
	startup.Log(cfg, tenantRepo, memoryRepo, embedMeta)

	r := gin.New()
	r.Use(apimiddleware.Logging())
	r.Use(apimiddleware.Recovery())
	r.Use(apimiddleware.CORS())

	health := handlers.NewHealthHandler()
	memory := handlers.NewMemoryHandler(memoryService, cfg.Postprocess.MaxAttempts)
	tenant := handlers.NewTenantHandler(tenantService)
	dashboardHandlers := dashboard.NewHandlers(memoryService, tenantService)

	r.Static("/static", "./web/static")

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/dashboard")
	})
	r.GET("/health", health.Get)
	r.GET("/dashboard", dashboardHandlers.Index)
	r.GET("/dashboard/memories", dashboardHandlers.Memories)
	r.POST("/dashboard/memories", dashboardHandlers.CreateMemory)
	r.POST("/dashboard/memories/:id/delete", dashboardHandlers.DeleteMemory)
	r.GET("/dashboard/tenants", dashboardHandlers.Tenants)
	r.POST("/dashboard/tenants", dashboardHandlers.CreateTenant)
	r.GET("/dashboard/stats", dashboardHandlers.Stats)

	v1 := r.Group("/v1")
	if cfg.Auth.Enabled {
		authenticator, err := apiauth.NewJWTAuthenticator(cfg.Auth.JWTSecret, cfg.Auth.Issuer)
		if err != nil {
			_ = closeEntityFactRepo()
			_ = db.Close()
			return nil, nil, fmt.Errorf("initialize auth: %w", err)
		}
		v1.Use(apiauth.Middleware(authenticator))
	}
	{
		v1.POST("/memory", memory.Store)
		v1.POST("/memory/batch", memory.StoreBatch)
		v1.POST("/memory/ingest", memory.Ingest)
		v1.POST("/memory/ingest/batch", memory.IngestBatch)
		v1.POST("/memory/search", memory.Search)
		v1.GET("/memory/jobs", memory.ListPostprocessJobs)
		v1.GET("/memory/jobs/:id", memory.GetPostprocessJob)
		v1.DELETE("/memory/:id", memory.Delete)

		v1.POST("/tenants", tenant.Create)
		v1.GET("/tenants/:id/stats", tenant.Stats)
	}

	return r, cleanup, nil
}
