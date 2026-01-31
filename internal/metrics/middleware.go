package metrics

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// GinMiddleware returns a gin middleware that records Prometheus HTTP metrics.
func GinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		c.Next()

		duration := time.Since(start).Seconds()
		status := fmt.Sprintf("%d", c.Writer.Status())
		path := normalizePath(c.FullPath())

		// If FullPath is empty (e.g. 404 no-route), use a fixed label.
		if path == "" {
			path = "unmatched"
		}

		HTTPRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		HTTPRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}

// normalizePath replaces Gin route params with their placeholder names
// to avoid cardinality explosion from unique UUIDs/IDs in metric labels.
// Gin's FullPath() already returns the template (e.g. /api2/repos/:repo_id),
// so this mainly handles edge cases.
func normalizePath(path string) string {
	if path == "" {
		return path
	}
	// Strip trailing slash for consistency
	return strings.TrimRight(path, "/")
}
