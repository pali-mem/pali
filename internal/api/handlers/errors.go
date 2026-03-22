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
	status := statusForError(err)
	switch status {
	case http.StatusConflict:
		c.JSON(status, gin.H{"error": "conflict"})
	case http.StatusInternalServerError:
		log.Printf("[pali-api] internal error: %v", err)
		c.JSON(status, gin.H{"error": "internal server error"})
	default:
		c.JSON(status, gin.H{"error": err.Error()})
	}
}

func writeBindError(c *gin.Context, err error) {
	status := statusForBindError(err)
	switch status {
	case http.StatusBadRequest:
		switch {
		case errors.Is(err, errInvalidJSONBody):
			c.JSON(status, gin.H{"error": errInvalidJSONBody.Error()})
		case errors.Is(err, errEmptyBatchItems):
			c.JSON(status, gin.H{"error": errEmptyBatchItems.Error()})
		default:
			c.JSON(status, gin.H{"error": err.Error()})
		}
	default:
		writeError(c, err)
	}
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrTenantMismatch):
		return http.StatusForbidden
	case strings.Contains(strings.ToLower(err.Error()), "constraint failed"):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func statusForBindError(err error) int {
	switch {
	case errors.Is(err, errInvalidJSONBody):
		return http.StatusBadRequest
	case errors.Is(err, errEmptyBatchItems):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrInvalidInput):
		return http.StatusBadRequest
	default:
		return statusForError(err)
	}
}
