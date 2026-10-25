package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/api/dto"
	apiauth "github.com/pali-mem/pali/internal/auth"
	coretenant "github.com/pali-mem/pali/internal/core/tenant"
	"github.com/pali-mem/pali/internal/domain"
)

// TenantHandler serves tenant endpoints.
type TenantHandler struct {
	service *coretenant.Service
}

// NewTenantHandler constructs a tenant handler.
func NewTenantHandler(service *coretenant.Service) *TenantHandler {
	return &TenantHandler{service: service}
}

// Create creates a new tenant.
func (h *TenantHandler) Create(c *gin.Context) {
	var req dto.CreateTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if err := enforceTenantAccess(c, req.ID); err != nil {
		writeError(c, err)
		return
	}

	created, err := h.service.Create(c.Request.Context(), domain.Tenant{
		ID:   req.ID,
		Name: req.Name,
	})
	if err != nil {
		writeError(c, err)
		return
	}

	c.JSON(http.StatusCreated, dto.CreateTenantResponse{
		ID:        created.ID,
		Name:      created.Name,
		CreatedAt: created.CreatedAt,
	})
}

// Stats returns tenant statistics.
func (h *TenantHandler) Stats(c *gin.Context) {
	tenantID := c.Param("id")
	if err := enforceTenantAccess(c, tenantID); err != nil {
		writeError(c, err)
		return
	}
	stats, err := h.service.Stats(c.Request.Context(), tenantID)
	if err != nil {
		writeError(c, err)
		return
	}

	byTier, byKind := stats.ByTier, stats.ByKind
	if byTier == nil {
		byTier = map[string]int64{}
	}
	if byKind == nil {
		byKind = map[string]int64{}
	}
	c.JSON(http.StatusOK, dto.TenantStatsResponse{
		TenantID:       tenantID,
		MemoryCount:    stats.MemoryCount,
		ByTier:         byTier,
		ByKind:         byKind,
		TotalRecalls:   stats.TotalRecalls,
		NewestMemoryAt: stats.NewestMemoryAt,
	})
}

const (
	defaultTenantListLimit = 100
	maxTenantListLimit     = 500
)

// List returns tenants with their memory counts. With a tenant-scoped token
// only the caller's own tenant is returned.
func (h *TenantHandler) List(c *gin.Context) {
	limit := defaultTenantListLimit
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid limit"})
			return
		}
		limit = min(parsed, maxTenantListLimit)
	}

	var tenants []coretenant.WithStats
	if authTenantID, scoped := apiauth.TenantIDFromGin(c); scoped {
		one, err := h.service.GetWithStats(c.Request.Context(), authTenantID)
		switch {
		case err == nil:
			tenants = []coretenant.WithStats{one}
		case !errors.Is(err, domain.ErrNotFound):
			writeError(c, err)
			return
		}
	} else {
		var err error
		tenants, err = h.service.ListWithStats(c.Request.Context(), limit)
		if err != nil {
			writeError(c, err)
			return
		}
	}

	out := make([]dto.TenantItemResponse, 0, len(tenants))
	for _, item := range tenants {
		out = append(out, dto.TenantItemResponse{
			ID:          item.Tenant.ID,
			Name:        item.Tenant.Name,
			CreatedAt:   item.Tenant.CreatedAt,
			MemoryCount: item.Stats.MemoryCount,
		})
	}
	c.JSON(http.StatusOK, dto.ListTenantsResponse{Items: out})
}
