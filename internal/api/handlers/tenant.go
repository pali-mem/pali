package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/vein05/pali/internal/api/dto"
	coretenant "github.com/vein05/pali/internal/core/tenant"
	"github.com/vein05/pali/internal/domain"
)

type TenantHandler struct {
	service *coretenant.Service
}

func NewTenantHandler(service *coretenant.Service) *TenantHandler {
	return &TenantHandler{service: service}
}

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

	c.JSON(http.StatusOK, dto.TenantStatsResponse{
		TenantID:    tenantID,
		MemoryCount: stats.MemoryCount,
	})
}
