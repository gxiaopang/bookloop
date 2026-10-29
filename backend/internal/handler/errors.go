package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

func writeServiceError(c *gin.Context, logger *slog.Logger, err error) {
	requestID := middleware.RequestIDFromContext(c)
	switch {
	case errors.Is(err, service.ErrValidation):
		c.JSON(http.StatusBadRequest, dto.ErrorResponse("VALIDATION_ERROR", "request validation failed", requestID))
	case errors.Is(err, service.ErrUsernameAlreadyExists):
		c.JSON(http.StatusConflict, dto.ErrorResponse("USERNAME_ALREADY_EXISTS", "username already exists", requestID))
	case errors.Is(err, service.ErrEmailAlreadyExists):
		c.JSON(http.StatusConflict, dto.ErrorResponse("EMAIL_ALREADY_EXISTS", "email already exists", requestID))
	case errors.Is(err, service.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse("INVALID_CREDENTIALS", "invalid username/email or password", requestID))
	case errors.Is(err, service.ErrUnauthorized):
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse("UNAUTHORIZED", "authentication required", requestID))
	case errors.Is(err, service.ErrTokenExpired):
		c.JSON(http.StatusUnauthorized, dto.ErrorResponse("TOKEN_EXPIRED", "access token has expired", requestID))
	case errors.Is(err, service.ErrUserDisabled):
		c.JSON(http.StatusForbidden, dto.ErrorResponse("USER_DISABLED", "user account is disabled", requestID))
	case errors.Is(err, service.ErrForbidden):
		c.JSON(http.StatusForbidden, dto.ErrorResponse("FORBIDDEN", "insufficient permissions", requestID))
	case errors.Is(err, service.ErrBookNotOwner):
		c.JSON(http.StatusForbidden, dto.ErrorResponse("BOOK_NOT_OWNER", "book is not owned by current user", requestID))
	case errors.Is(err, service.ErrBookNotFound):
		c.JSON(http.StatusNotFound, dto.ErrorResponse("BOOK_NOT_FOUND", "book not found", requestID))
	case errors.Is(err, service.ErrBookImageNotFound):
		c.JSON(http.StatusNotFound, dto.ErrorResponse("BOOK_IMAGE_NOT_FOUND", "book image not found", requestID))
	case errors.Is(err, service.ErrBookStateConflict):
		c.JSON(http.StatusConflict, dto.ErrorResponse("BOOK_STATE_CONFLICT", "book state does not allow this operation", requestID))
	case errors.Is(err, service.ErrBookImageLimitReached):
		c.JSON(http.StatusConflict, dto.ErrorResponse("BOOK_IMAGE_LIMIT_REACHED", "book cannot have more than six images", requestID))
	case errors.Is(err, service.ErrBookImageRequired):
		c.JSON(http.StatusConflict, dto.ErrorResponse("BOOK_IMAGE_REQUIRED", "book requires at least one image", requestID))
	case errors.Is(err, service.ErrBookContactRequired):
		c.JSON(http.StatusConflict, dto.ErrorResponse("BOOK_CONTACT_REQUIRED", "book requires a WeChat contact QR code", requestID))
	case errors.Is(err, service.ErrImageTooLarge):
		c.JSON(http.StatusRequestEntityTooLarge, dto.ErrorResponse("IMAGE_TOO_LARGE", "image exceeds the configured size limit", requestID))
	case errors.Is(err, service.ErrInvalidImage):
		c.JSON(http.StatusUnprocessableEntity, dto.ErrorResponse("INVALID_IMAGE", "image must be a valid JPEG or PNG", requestID))
	case errors.Is(err, service.ErrBookNotAvailable):
		c.JSON(http.StatusConflict, dto.ErrorResponse("BOOK_NOT_AVAILABLE", "book is not available for purchase", requestID))
	case errors.Is(err, service.ErrCannotPurchaseOwnBook):
		c.JSON(http.StatusForbidden, dto.ErrorResponse("CANNOT_PURCHASE_OWN_BOOK", "users cannot purchase their own books", requestID))
	case errors.Is(err, service.ErrOrderNotFound):
		c.JSON(http.StatusNotFound, dto.ErrorResponse("ORDER_NOT_FOUND", "order not found", requestID))
	case errors.Is(err, service.ErrOrderAccessDenied):
		c.JSON(http.StatusForbidden, dto.ErrorResponse("ORDER_ACCESS_DENIED", "current user cannot access this order", requestID))
	case errors.Is(err, service.ErrOrderStateConflict):
		c.JSON(http.StatusConflict, dto.ErrorResponse("ORDER_STATE_CONFLICT", "order state does not allow this operation", requestID))
	case errors.Is(err, service.ErrIdempotencyKeyReused):
		c.JSON(http.StatusConflict, dto.ErrorResponse("IDEMPOTENCY_KEY_REUSED", "idempotency key was already used for a different request", requestID))
	case errors.Is(err, service.ErrRequestInProgress):
		c.Header("Retry-After", "1")
		c.JSON(http.StatusConflict, dto.ErrorResponse("REQUEST_IN_PROGRESS", "an identical request is still processing", requestID))
	case errors.Is(err, service.ErrDependencyUnavailable):
		c.JSON(http.StatusServiceUnavailable, dto.ErrorResponse("SERVICE_UNAVAILABLE", "required dependency is temporarily unavailable", requestID))
	case errors.Is(err, service.ErrNotificationNotFound):
		c.JSON(http.StatusNotFound, dto.ErrorResponse("NOTIFICATION_NOT_FOUND", "notification not found", requestID))
	default:
		logger.Error("request failed with internal error", "request_id", requestID, "error", err)
		c.JSON(http.StatusInternalServerError, dto.ErrorResponse("INTERNAL_ERROR", "internal server error", requestID))
	}
}
