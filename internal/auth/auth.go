// Package auth contains request-scoped authentication helpers and claims.
package auth

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// TenantIDContextKey stores the authenticated tenant ID in Gin contexts.
const TenantIDContextKey = "auth.tenant_id"

// TenantClaims carries the tenant identity inside JWTs.
type TenantClaims struct {
	TenantID string `json:"tenant_id"`
	jwt.RegisteredClaims
}

// Authenticator validates a bearer token and returns its tenant ID.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (tenantID string, err error)
}

// TenantIDFromGin returns the authenticated tenant ID from a Gin context.
func TenantIDFromGin(c *gin.Context) (string, bool) {
	raw, ok := c.Get(TenantIDContextKey)
	if !ok {
		return "", false
	}
	tenantID, ok := raw.(string)
	return tenantID, ok && tenantID != ""
}
