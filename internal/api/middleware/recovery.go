package middleware

import "github.com/gin-gonic/gin"

// Recovery returns Gin's panic recovery middleware.
func Recovery() gin.HandlerFunc {
	return gin.Recovery()
}
