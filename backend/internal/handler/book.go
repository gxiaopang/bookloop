package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

const multipartOverheadAllowance = 1 << 20

type Book struct {
	service        *service.Book
	logger         *slog.Logger
	uploadMaxBytes int64
}

func NewBook(bookService *service.Book, logger *slog.Logger, uploadMaxBytes int64) *Book {
	return &Book{service: bookService, logger: logger, uploadMaxBytes: uploadMaxBytes}
}

func (h *Book) List(c *gin.Context) {
	query, err := parseBookListQuery(c)
	if err != nil {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	response, err := h.service.ListPublic(c.Request.Context(), query)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) Get(c *gin.Context) {
	bookID, ok := parseID(c, "book_id")
	if !ok {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	response, err := h.service.GetPublic(c.Request.Context(), bookID)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) Create(c *gin.Context) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	var request dto.CreateBookRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	response, err := h.service.Create(c.Request.Context(), user.ID, request)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusCreated, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) Update(c *gin.Context) {
	user, bookID, ok := h.currentUserAndBookID(c)
	if !ok {
		return
	}
	var request dto.UpdateBookRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	response, err := h.service.Update(c.Request.Context(), user.ID, bookID, request)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) UploadImage(c *gin.Context) {
	user, bookID, ok := h.currentUserAndBookID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.uploadMaxBytes+multipartOverheadAllowance)
	fileHeader, err := c.FormFile("image")
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeServiceError(c, h.logger, service.ErrImageTooLarge)
			return
		}
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	if c.Request.MultipartForm != nil {
		defer func() {
			if cleanupErr := c.Request.MultipartForm.RemoveAll(); cleanupErr != nil {
				h.logger.Warn("failed to clean multipart temporary files", "error", cleanupErr)
			}
		}()
	}
	if fileHeader.Size > h.uploadMaxBytes {
		writeServiceError(c, h.logger, service.ErrImageTooLarge)
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("failed to close uploaded image", "error", closeErr)
		}
	}()
	response, err := h.service.UploadImage(c.Request.Context(), user.ID, bookID, file)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusCreated, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) DeleteImage(c *gin.Context) {
	user, bookID, ok := h.currentUserAndBookID(c)
	if !ok {
		return
	}
	imageID, ok := parseID(c, "image_id")
	if !ok {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	if err := h.service.DeleteImage(c.Request.Context(), user.ID, bookID, imageID); err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(gin.H{"deleted": true}, middleware.RequestIDFromContext(c)))
}

func (h *Book) ReorderImages(c *gin.Context) {
	user, bookID, ok := h.currentUserAndBookID(c)
	if !ok {
		return
	}
	var request dto.ImageOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	response, err := h.service.ReorderImages(c.Request.Context(), user.ID, bookID, request.ImageIDs)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) PutOnShelf(c *gin.Context) {
	h.changeOwnerStatus(c, h.service.PutOnShelf)
}

func (h *Book) TakeOffShelf(c *gin.Context) {
	h.changeOwnerStatus(c, h.service.TakeOffShelf)
}

func (h *Book) changeOwnerStatus(
	c *gin.Context,
	operation func(context.Context, uint64, uint64) (*dto.BookResponse, error),
) {
	user, bookID, ok := h.currentUserAndBookID(c)
	if !ok {
		return
	}
	response, err := operation(c.Request.Context(), user.ID, bookID)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) ListMine(c *gin.Context) {
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
	response, err := h.service.ListMine(c.Request.Context(), user.ID, page, pageSize)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) GetMine(c *gin.Context) {
	user, bookID, ok := h.currentUserAndBookID(c)
	if !ok {
		return
	}
	response, err := h.service.GetMine(c.Request.Context(), user.ID, bookID)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) Block(c *gin.Context) {
	h.changeAdminStatus(c, h.service.Block)
}

func (h *Book) Unblock(c *gin.Context) {
	h.changeAdminStatus(c, h.service.Unblock)
}

func (h *Book) changeAdminStatus(
	c *gin.Context,
	operation func(context.Context, *model.User, uint64) (*dto.BookResponse, error),
) {
	user, ok := middleware.CurrentUserFromContext(c)
	if !ok {
		writeServiceError(c, h.logger, service.ErrUnauthorized)
		return
	}
	bookID, ok := parseID(c, "book_id")
	if !ok {
		writeServiceError(c, h.logger, service.ErrValidation)
		return
	}
	response, err := operation(c.Request.Context(), user, bookID)
	if err != nil {
		writeServiceError(c, h.logger, err)
		return
	}
	c.JSON(http.StatusOK, dto.SuccessResponse(response, middleware.RequestIDFromContext(c)))
}

func (h *Book) currentUserAndBookID(c *gin.Context) (*model.User, uint64, bool) {
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

func parseBookListQuery(c *gin.Context) (dto.BookListQuery, error) {
	if !onlyQueryParameters(c, "keyword", "condition", "sort", "page", "page_size") {
		return dto.BookListQuery{}, service.ErrValidation
	}
	page, err := optionalPositiveInt(c.Query("page"))
	if err != nil {
		return dto.BookListQuery{}, err
	}
	pageSize, err := optionalPositiveInt(c.Query("page_size"))
	if err != nil {
		return dto.BookListQuery{}, err
	}
	return dto.BookListQuery{
		Keyword:   c.Query("keyword"),
		Condition: c.Query("condition"),
		Sort:      c.Query("sort"),
		Page:      page,
		PageSize:  pageSize,
	}, nil
}

func onlyQueryParameters(c *gin.Context, allowed ...string) bool {
	known := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		known[key] = struct{}{}
	}
	for key := range c.Request.URL.Query() {
		if _, ok := known[key]; !ok {
			return false
		}
	}
	return true
}

func optionalPositiveInt(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, errors.New("value must be a positive integer")
	}
	return parsed, nil
}

func parseID(c *gin.Context, name string) (uint64, bool) {
	value, err := strconv.ParseUint(c.Param(name), 10, 64)
	return value, err == nil && value > 0
}
