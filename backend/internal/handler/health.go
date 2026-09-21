package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

type Health struct {
	readiness *service.Readiness
	logger    *slog.Logger
}

func NewHealth(readiness *service.Readiness, logger *slog.Logger) *Health {
	return &Health{readiness: readiness, logger: logger}
}

func (h *Health) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, dto.SuccessResponse(
		gin.H{"status": "alive"},
		middleware.RequestIDFromContext(c),
	))
}

func (h *Health) Readiness(c *gin.Context) {
	requestID := middleware.RequestIDFromContext(c)
	result := h.readiness.Check(c.Request.Context())

	mysqlStatus := "up"
	redisStatus := "up"
	if result.MySQLError != nil {
		mysqlStatus = "down"
		h.logger.Error("readiness dependency unavailable",
			"request_id", requestID,
			"dependency", "mysql",
			"error", result.MySQLError,
		)
	}
	if result.RedisError != nil {
		redisStatus = "down"
		h.logger.Warn("readiness dependency unavailable",
			"request_id", requestID,
			"dependency", "redis",
			"error", result.RedisError,
		)
	}

	overallStatus := "ready"
	if !result.Ready() {
		overallStatus = "not_ready"
	} else if result.Degraded() {
		overallStatus = "degraded"
	}
	data := gin.H{
		"status": overallStatus,
		"dependencies": gin.H{
			"mysql": gin.H{"status": mysqlStatus, "required": true},
			"redis": gin.H{"status": redisStatus, "required": result.RedisRequired},
		},
	}

	if !result.Ready() {
		response := dto.ErrorResponse("SERVICE_UNAVAILABLE", "service is not ready", requestID)
		response.Data = data
		c.JSON(http.StatusServiceUnavailable, response)
		return
	}

	c.JSON(http.StatusOK, dto.SuccessResponse(data, requestID))
}
