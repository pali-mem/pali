package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pali-mem/pali/internal/telemetry"
)

func Telemetry(service *telemetry.Service) gin.HandlerFunc {
	if service == nil {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		started := time.Now()
		trackRequest := telemetry.ShouldTrackRequestPath(c.Request.URL.Path)
		if trackRequest {
			service.RequestStarted()
			defer service.RequestFinished()
		}

		c.Next()

		path := strings.TrimSpace(c.FullPath())
		if path == "" {
			path = c.Request.URL.Path
		}
		if !telemetry.ShouldTrackRequestPath(path) {
			return
		}
		tenantID, _ := c.Get(telemetry.TenantContextKey)
		tenantValue, _ := tenantID.(string)

		service.RecordRequest(telemetry.RequestObservation{
			At:       time.Now().UTC(),
			Method:   c.Request.Method,
			Path:     path,
			TenantID: strings.TrimSpace(tenantValue),
			Status:   c.Writer.Status(),
			Latency:  time.Since(started),
		})
	}
}
