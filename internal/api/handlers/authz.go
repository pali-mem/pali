package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"
	apiauth "github.com/vein05/pali/internal/auth"
	"github.com/vein05/pali/internal/domain"
)

func enforceTenantAccess(c *gin.Context, requestedTenantID string) error {
	requestedTenantID = strings.TrimSpace(requestedTenantID)
	if requestedTenantID == "" {
		return domain.ErrInvalidInput
	}

	authTenantID, ok := apiauth.TenantIDFromGin(c)
	if !ok {
		// Auth disabled.
		return nil
	}
	if authTenantID != requestedTenantID {
		return domain.ErrTenantMismatch
	}
	return nil
}
