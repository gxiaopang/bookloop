package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
)

func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				requestID := RequestIDFromContext(c)
				logger.Error("panic recovered",
					"request_id", requestID,
					"method", c.Request.Method,
					"path", c.Request.URL.Path,
					"panic", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError,
					dto.ErrorResponse("INTERNAL_ERROR", "internal server error", requestID),
				)
			}
		}()

		c.Next()
	}
}
