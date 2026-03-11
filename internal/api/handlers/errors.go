package handlers

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/domain"
)

var (
	errInvalidJSONBody = errors.New("invalid JSON body")
	errEmptyBatchItems = errors.New("items must not be empty")
)

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, domain.ErrTenantMismatch):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case strings.Contains(strings.ToLower(err.Error()), "constraint failed"):
		c.JSON(http.StatusConflict, gin.H{"error": "conflict"})
	default:
		log.Printf("[pali-api] internal error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

func writeBindError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errInvalidJSONBody):
		c.JSON(http.StatusBadRequest, gin.H{"error": errInvalidJSONBody.Error()})
	case errors.Is(err, errEmptyBatchItems):
		c.JSON(http.StatusBadRequest, gin.H{"error": errEmptyBatchItems.Error()})
	case errors.Is(err, domain.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		writeError(c, err)
	}
}
