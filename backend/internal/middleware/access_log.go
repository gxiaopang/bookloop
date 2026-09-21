package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		attributes := []any{
			"request_id", RequestIDFromContext(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration", time.Since(startedAt),
		}
		if user, ok := CurrentUserFromContext(c); ok {
			attributes = append(attributes, "user_id", user.ID)
		}
		logger.Info("http request completed", attributes...)
	}
}
