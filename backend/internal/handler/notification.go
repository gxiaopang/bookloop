package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

type Notification struct {
	service *service.Notification
	logger  *slog.Logger
}

func NewNotification(notificationService *service.Notification, logger *slog.Logger) *Notification {
	return &Notification{service: notificationService, logger: logger}
}

func (h *Notification) List(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	if !onlyQueryParameters(c, "page", "page_size", "unread_only") {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	page, pageSize, ok := parsePagination(c, h.logger)
	if !ok {
		return
	}
	unreadOnly := false
	if value := c.Query("unread_only"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			writeServiceError(c, h.logger, service.ErrValidation)
			return
		}
		unreadOnly = parsed
	}
	response, err := h.service.List(c.Request.Context(), user.ID, unreadOnly, page, pageSize)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Notification) MarkRead(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	notificationID, ok := parseID(c, "notification_id")
	if !ok {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	if err := h.service.MarkRead(c.Request.Context(), user.ID, notificationID); err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(gin.H{"read": true}, middleware.RequestIDFromContext(c)))
}

func (h *Notification) MarkAllRead(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	if err := h.service.MarkAllRead(c.Request.Context(), user.ID); err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(gin.H{"all_read": true}, middleware.RequestIDFromContext(c)))
}

type MessagingStatus struct {
	service *service.MessagingStatus
	logger  *slog.Logger
}

func NewMessagingStatus(statusService *service.MessagingStatus, logger *slog.Logger) *MessagingStatus {
	return &MessagingStatus{service: statusService, logger: logger}
}

func (h *MessagingStatus) Get(c *gin.Context) {
	response, err := h.service.Get(c.Request.Context())
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}
