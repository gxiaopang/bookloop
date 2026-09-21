package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

type Auth struct {
	service *service.Auth
	logger  *slog.Logger
}

func NewAuth(authService *service.Auth, logger *slog.Logger) *Auth {
	return &Auth{service: authService, logger: logger}
}

func (h *Auth) Register(c *gin.Context) {
	var request dto.RegisterRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}

	user, err := h.service.Register(c.Request.Context(), request)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusCreated, dto.SuccessResponse(
		userResponse(user),
		middleware.RequestIDFromContext(c),
	))
}

func (h *Auth) Login(c *gin.Context) {
	var request dto.LoginRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}

	result, err := h.service.Login(c.Request.Context(), request)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(dto.LoginResponse{
		AccessToken: result.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   result.ExpiresIn,
		User:        userResponse(result.User),
	}, middleware.RequestIDFromContext(c)))
}

func (h *Auth) CurrentUser(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(
		userResponse(user),
		middleware.RequestIDFromContext(c),
	))
}

func userResponse(user *model.User) dto.UserResponse {
	return dto.UserResponse{
		ID:        user.ID,
		Username:  user.Username,
		Email:     user.Email,
		Role:      string(user.Role),
		Status:    string(user.Status),
		CreatedAt: user.CreatedAt,
	}
}
