package auth

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// Middleware validates bearer tokens and stores the tenant ID in context.
func Middleware(a Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		tenantID, err := a.Authenticate(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		c.Set(TenantIDContextKey, tenantID)
		c.Next()
	}
}
