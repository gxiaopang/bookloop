package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

const currentUserKey = "current_user"

type Authentication struct {
	service *service.Auth
	logger  *slog.Logger
}

func NewAuthentication(authService *service.Auth, logger *slog.Logger) *Authentication {
	return &Authentication{service: authService, logger: logger}
}

func (m *Authentication) RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		rawToken, ok := bearerToken(c.GetHeader("Authorization"))
		if !ok {
			m.abort(c, service.ErrUnauthorized)
			return
		}

		user, err := m.service.Authenticate(c.Request.Context(), rawToken)
		if err != nil {
			m.abort(c, err)
			return
		}
		c.Set(currentUserKey, user)
		c.Next()
	}
}

func RequireRole(role model.UserRole) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := CurrentUserFromContext(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, dto.ErrorResponse(
				"UNAUTHORIZED", "authentication required", RequestIDFromContext(c),
			))
			return
		}
		if user.Role != role {
			c.AbortWithStatusJSON(http.StatusForbidden, dto.ErrorResponse(
				"FORBIDDEN", "insufficient permissions", RequestIDFromContext(c),
			))
			return
		}
		c.Next()
	}
}

func CurrentUserFromContext(c *gin.Context) (*model.User, bool) {
	value, exists := c.Get(currentUserKey)
	if !exists {
		return nil, false
	}
	user, ok := value.(*model.User)
	return user, ok && user != nil
}

func bearerToken(header string) (string, bool) {
	parts := strings.Fields(header)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return "", false
	}
	return parts[1], true
}

func (m *Authentication) abort(c *gin.Context, err error) {
	requestID := RequestIDFromContext(c)
	switch {
	case errors.Is(err, service.ErrTokenExpired):
		c.AbortWithStatusJSON(http.StatusUnauthorized, dto.ErrorResponse(
			"TOKEN_EXPIRED", "access token has expired", requestID,
		))
	case errors.Is(err, service.ErrUserDisabled):
		c.AbortWithStatusJSON(http.StatusForbidden, dto.ErrorResponse(
			"USER_DISABLED", "user account is disabled", requestID,
		))
	case errors.Is(err, service.ErrUnauthorized):
		c.AbortWithStatusJSON(http.StatusUnauthorized, dto.ErrorResponse(
			"UNAUTHORIZED", "invalid or missing access token", requestID,
		))
	default:
		m.logger.Error("authentication failed with internal error", "request_id", requestID, "error", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, dto.ErrorResponse(
			"INTERNAL_ERROR", "internal server error", requestID,
		))
	}
}
