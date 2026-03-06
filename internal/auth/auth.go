package auth

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const TenantIDContextKey = "auth.tenant_id"

type TenantClaims struct {
	TenantID string `json:"tenant_id"`
	jwt.RegisteredClaims
}

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (tenantID string, err error)
}

func TenantIDFromGin(c *gin.Context) (string, bool) {
	raw, ok := c.Get(TenantIDContextKey)
	if !ok {
		return "", false
	}
	tenantID, ok := raw.(string)
	return tenantID, ok && tenantID != ""
}
