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

type Favorite struct {
	service *service.Favorite
	logger  *slog.Logger
}

func NewFavorite(favoriteService *service.Favorite, logger *slog.Logger) *Favorite {
	return &Favorite{service: favoriteService, logger: logger}
}

func (h *Favorite) List(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	if !onlyQueryParameters(c, "page", "page_size") {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	page, err := optionalPositiveInt(c.Query("page"))
	if err != nil {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	pageSize, err := optionalPositiveInt(c.Query("page_size"))
	if err != nil {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	response, err := h.service.List(c.Request.Context(), user.ID, page, pageSize)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Favorite) Add(c *gin.Context) {
	user, bookID, ok := h.currentUserAndBookID(c)
	if !ok {
		return
	}
	if err := h.service.Add(c.Request.Context(), user.ID, bookID); err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(gin.H{"favorited": true}, middleware.RequestIDFromContext(c)))
}

func (h *Favorite) Remove(c *gin.Context) {
	user, bookID, ok := h.currentUserAndBookID(c)
	if !ok {
		return
	}
	if err := h.service.Remove(c.Request.Context(), user.ID, bookID); err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(gin.H{"favorited": false}, middleware.RequestIDFromContext(c)))
}

func (h *Favorite) currentUserAndBookID(c *gin.Context) (*model.User, uint64, bool) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return nil, 0, false
	}
	bookID, ok := parseID(c, "book_id")
	if !ok {
		writeServiceError(c, h.logger, service.ErrValidation)
		return nil, 0, false
	}
	return user, bookID, true
}
