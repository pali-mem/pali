package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// Logging returns request logging middleware with dashboard noise filtered out.
func Logging() gin.HandlerFunc {
	return gin.LoggerWithConfig(gin.LoggerConfig{
		Skip: func(c *gin.Context) bool {
			return strings.HasPrefix(c.Request.URL.Path, "/dashboard/analytics")
		},
	})
}
