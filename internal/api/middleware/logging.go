package middleware

import "github.com/gin-gonic/gin"

func Logging() gin.HandlerFunc {
	return gin.Logger()
}
