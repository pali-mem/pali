// Package handlers provides HTTP route handlers for the API.
package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// HealthHandler serves health endpoints.
type HealthHandler struct{}

// NewHealthHandler constructs a health handler.
func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

// Get returns the service health status.
func (h *HealthHandler) Get(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}
