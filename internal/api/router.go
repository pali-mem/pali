package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/vein05/pali/internal/api/handlers"
	apimiddleware "github.com/vein05/pali/internal/api/middleware"
	apiauth "github.com/vein05/pali/internal/auth"
	"github.com/vein05/pali/internal/config"
	corememory "github.com/vein05/pali/internal/core/memory"
	coretenant "github.com/vein05/pali/internal/core/tenant"
	"github.com/vein05/pali/internal/dashboard"
	"github.com/vein05/pali/internal/embeddings"
	sqliterepo "github.com/vein05/pali/internal/repository/sqlite"
	"github.com/vein05/pali/internal/wiring"
)

func NewRouter(cfg config.Config) (*gin.Engine, func() error, error) {
	if gin.Mode() == "" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := sqliterepo.Open(context.Background(), cfg.Database.SQLiteDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open sqlite for router: %w", err)
	}
	cleanup := func() error { return db.Close() }

	tenantRepo := sqliterepo.NewTenantRepository(db)
	memoryRepo := sqliterepo.NewMemoryRepository(db)
	entityFactRepo := sqliterepo.NewEntityFactRepository(db)
	vectorStore, err := wiring.BuildVectorStore(cfg, db)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}

	embedder, err := embeddings.Build(cfg)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	scorer, err := wiring.BuildImportanceScorer(cfg)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	infoParser, err := wiring.BuildInfoParser(cfg)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	serviceOptions := []corememory.ServiceOption{
		corememory.StructuredMemoryOptions{
			Enabled:               cfg.StructuredMemory.Enabled,
			DualWriteObservations: cfg.StructuredMemory.DualWriteObservations,
			DualWriteEvents:       cfg.StructuredMemory.DualWriteEvents,
			QueryRoutingEnabled:   cfg.StructuredMemory.QueryRoutingEnabled,
			MaxObservations:       cfg.StructuredMemory.MaxObservations,
		},
		corememory.RankingOptions{
			Algorithm: cfg.Retrieval.Scoring.Algorithm,
			WAL: corememory.WALWeights{
				Recency:    cfg.Retrieval.Scoring.WAL.Recency,
				Relevance:  cfg.Retrieval.Scoring.WAL.Relevance,
				Importance: cfg.Retrieval.Scoring.WAL.Importance,
			},
			Match: corememory.MatchWeights{
				Recency:      cfg.Retrieval.Scoring.Match.Recency,
				Relevance:    cfg.Retrieval.Scoring.Match.Relevance,
				Importance:   cfg.Retrieval.Scoring.Match.Importance,
				QueryOverlap: cfg.Retrieval.Scoring.Match.QueryOverlap,
				Routing:      cfg.Retrieval.Scoring.Match.Routing,
			},
		},
		corememory.ParserOptions{
			Enabled:         cfg.Parser.Enabled,
			Provider:        cfg.Parser.Provider,
			Model:           cfg.Parser.OllamaModel,
			StoreRawTurn:    cfg.Parser.StoreRawTurn,
			MaxFacts:        cfg.Parser.MaxFacts,
			DedupeThreshold: cfg.Parser.DedupeThreshold,
			UpdateThreshold: cfg.Parser.UpdateThreshold,
		},
		corememory.WithLogger(log.Default()),
		corememory.WithDebug(cfg.Logging.DevVerbose, cfg.Logging.Progress),
	}
	if infoParser != nil {
		serviceOptions = append(serviceOptions, corememory.WithInfoParser(infoParser))
	}
	serviceOptions = append(serviceOptions, corememory.WithEntityFactRepository(entityFactRepo))
	memoryService := corememory.NewService(memoryRepo, tenantRepo, vectorStore, embedder, scorer, serviceOptions...)
	tenantService := coretenant.NewService(tenantRepo)
	logStartup(cfg, db)

	r := gin.New()
	r.Use(apimiddleware.Logging())
	r.Use(apimiddleware.Recovery())
	r.Use(apimiddleware.CORS())

	health := handlers.NewHealthHandler()
	memory := handlers.NewMemoryHandler(memoryService)
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
			_ = db.Close()
			return nil, nil, fmt.Errorf("initialize auth: %w", err)
		}
		v1.Use(apiauth.Middleware(authenticator))
	}
	{
		v1.POST("/memory", memory.Store)
		v1.POST("/memory/batch", memory.StoreBatch)
		v1.POST("/memory/search", memory.Search)
		v1.DELETE("/memory/:id", memory.Delete)

		v1.POST("/tenants", tenant.Create)
		v1.GET("/tenants/:id/stats", tenant.Stats)
	}

	return r, cleanup, nil
}

func logStartup(cfg config.Config, db *sql.DB) {
	logger := log.Default()
	logger.Printf("[pali-startup] pid=%d port=%d db=%s", os.Getpid(), cfg.Server.Port, cfg.Database.SQLiteDSN)
	logger.Printf(
		"[pali-startup] embedder=%s model=%s provider=%s",
		cfg.Embedding.Provider,
		cfg.Embedding.OllamaModel,
		cfg.Embedding.OllamaBaseURL,
	)
	var tenantCount int64
	var memoryCount int64
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM tenants").Scan(&tenantCount); err != nil {
		logger.Printf("[pali-startup] tenant_count_error=%v", err)
		return
	}
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM memories").Scan(&memoryCount); err != nil {
		logger.Printf("[pali-startup] memory_count_error=%v", err)
		return
	}
	logger.Printf("[pali-startup] tenant_count=%d memory_count=%d", tenantCount, memoryCount)
}
