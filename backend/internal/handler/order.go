package handler

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

type Order struct {
	service *service.Order
	logger  *slog.Logger
}

const orderNumberAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func NewOrder(orderService *service.Order, logger *slog.Logger) *Order {
	return &Order{service: orderService, logger: logger}
}

func (h *Order) Create(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	var request dto.CreateOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	result, err := h.service.Create(c.Request.Context(), user.ID, request.BookID, c.GetHeader("Idempotency-Key"))
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, dto.SuccessResponse(result.Order, middleware.RequestIDFromContext(c)))
}

func (h *Order) List(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	if !onlyQueryParameters(c, "as", "page", "page_size") {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	page, pageSize, ok := parsePagination(c, h.logger)
	if !ok {
		return
	}
	response, err := h.service.List(c.Request.Context(), user.ID, dto.OrderListQuery{
		As:       c.Query("as"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Order) Get(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	orderNo, ok := parseOrderNo(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	response, err := h.service.Get(c.Request.Context(), user, orderNo)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Order) Cancel(c *gin.Context) {
	h.action(c, h.service.Cancel)
}

func (h *Order) Complete(c *gin.Context) {
	h.action(c, h.service.Complete)
}

func (h *Order) action(
	c *gin.Context,
	operation func(context.Context, uint64, string) (*dto.OrderResponse, error),
) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	orderNo, ok := parseOrderNo(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	response, err := operation(c.Request.Context(), user.ID, orderNo)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Order) ListAll(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	if !onlyQueryParameters(c, "page", "page_size") {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	page, pageSize, ok := parsePagination(c, h.logger)
	if !ok {
		return
	}
	response, err := h.service.ListAll(c.Request.Context(), user, page, pageSize)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func parsePagination(c *gin.Context, logger *slog.Logger) (int, int, bool) {
	page, err := optionalPositiveInt(c.Query("page"))
	if err != nil {
		writeServiceError(c, logger, service.ErrValidation)
		return 0, 0, false
	}
	pageSize, err := optionalPositiveInt(c.Query("page_size"))
	if err != nil {
		writeServiceError(c, logger, service.ErrValidation)
		return 0, 0, false
	}
	return page, pageSize, true
}

func parseOrderNo(c *gin.Context) (string, bool) {
	orderNo := c.Param("order_no")
	if len(orderNo) != 26 {
		return "", false
	}
	for index := range len(orderNo) {
		if !strings.ContainsRune(orderNumberAlphabet, rune(orderNo[index])) {
			return "", false
		}
	}
	return orderNo, true
}
