package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/storage"
)

const (
	maxBookTitleLength       = 200
	maxBookDescriptionLength = 10_000
	maxBookKeywordLength     = 100
	maxBookImages            = 6
	defaultPage              = 1
	defaultPageSize          = 20
	maxPageSize              = 100
)

var pricePattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,7})(\.[0-9]{1,2})?$`)

type bookRepository interface {
	Create(context.Context, *model.Book) error
	FindByID(context.Context, uint64) (*model.Book, error)
	FindPublicByID(context.Context, uint64) (*model.Book, []model.BookImage, error)
	ListPublic(context.Context, dto.BookListQuery) ([]repository.BookListRecord, int64, error)
	ListBySeller(context.Context, uint64, int, int) ([]repository.BookListRecord, int64, error)
	ListImages(context.Context, uint64) ([]model.BookImage, error)
	WithinTransaction(context.Context, func(repository.BookStore) error) error
}

type imageStorage interface {
	Stage(context.Context, io.Reader) (*storage.StagedImage, error)
	Commit(context.Context, *storage.StagedImage) (string, error)
	Discard(*storage.StagedImage) error
	Delete(string) error
}

type Book struct {
	books  bookRepository
	images imageStorage
	logger *slog.Logger
}

func NewBook(books bookRepository, images imageStorage, logger *slog.Logger) *Book {
	return &Book{books: books, images: images, logger: logger}
}

func (s *Book) Create(ctx context.Context, sellerID uint64, request dto.CreateBookRequest) (*dto.BookResponse, error) {
	title, description, price, condition, err := validateBookInput(
		request.Title, request.Description, request.Price, request.Condition,
	)
	if err != nil {
		return nil, err
	}
	book := &model.Book{
		SellerID:    sellerID,
		Title:       title,
		Description: description,
		Price:       price,
		Condition:   condition,
		Status:      model.BookStatusOffShelf,
	}
	if err := s.books.Create(ctx, book); err != nil {
		return nil, err
	}
	response := bookResponse(book, []model.BookImage{})
	return &response, nil
}

func (s *Book) Update(ctx context.Context, sellerID, bookID uint64, request dto.UpdateBookRequest) (*dto.BookResponse, error) {
	if request.Title == nil && request.Description == nil && request.Price == nil && request.Condition == nil {
		return nil, ErrValidation
	}
	var updated *model.Book
	err := s.books.WithinTransaction(ctx, func(store repository.BookStore) error {
		book, err := store.FindByIDForUpdate(ctx, bookID)
		if err != nil {
			return mapBookRepositoryError(err)
		}
		if book.SellerID != sellerID {
			return ErrBookNotOwner
		}
		if !bookDetailsMutable(book.Status) {
			return ErrBookStateConflict
		}
		if request.Title != nil {
			title := strings.TrimSpace(*request.Title)
			if title == "" || utf8.RuneCountInString(title) > maxBookTitleLength {
				return ErrValidation
			}
			book.Title = title
		}
		if request.Description != nil {
			description, err := normalizeDescription(*request.Description)
			if err != nil {
				return err
			}
			book.Description = description
		}
		if request.Price != nil {
			price, err := normalizePrice(*request.Price)
			if err != nil {
				return err
			}
			book.Price = price
		}
		if request.Condition != nil {
			condition, ok := parseBookCondition(*request.Condition)
			if !ok {
				return ErrValidation
			}
			book.Condition = condition
		}
		if err := store.UpdateDetails(ctx, book); err != nil {
			return err
		}
		updated = book
		return nil
	})
	if err != nil {
		return nil, err
	}
	images, err := s.books.ListImages(ctx, bookID)
	if err != nil {
		return nil, err
	}
	response := bookResponse(updated, images)
	return &response, nil
}

func (s *Book) ListPublic(ctx context.Context, query dto.BookListQuery) (*dto.BookListResponse, error) {
	normalized, err := normalizeBookListQuery(query)
	if err != nil {
		return nil, err
	}
	records, total, err := s.books.ListPublic(ctx, normalized)
	if err != nil {
		return nil, err
	}
	return listResponse(records, normalized.Page, normalized.PageSize, total), nil
}

func (s *Book) GetPublic(ctx context.Context, bookID uint64) (*dto.BookResponse, error) {
	book, images, err := s.books.FindPublicByID(ctx, bookID)
	if err != nil {
		return nil, mapBookRepositoryError(err)
	}
	response := bookResponse(book, images)
	return &response, nil
}

func (s *Book) GetMine(ctx context.Context, sellerID, bookID uint64) (*dto.BookResponse, error) {
	book, err := s.books.FindByID(ctx, bookID)
	if err != nil {
		return nil, mapBookRepositoryError(err)
	}
	if book.SellerID != sellerID {
		return nil, ErrBookNotOwner
	}
	images, err := s.books.ListImages(ctx, bookID)
	if err != nil {
		return nil, err
	}
	response := bookResponse(book, images)
	return &response, nil
}

func (s *Book) ListMine(ctx context.Context, sellerID uint64, page, pageSize int) (*dto.BookListResponse, error) {
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return nil, err
	}
	records, total, err := s.books.ListBySeller(ctx, sellerID, page, pageSize)
	if err != nil {
		return nil, err
	}
	return listResponse(records, page, pageSize, total), nil
}

func (s *Book) UploadImage(ctx context.Context, sellerID, bookID uint64, source io.Reader) (*dto.BookImageResponse, error) {
	book, err := s.books.FindByID(ctx, bookID)
	if err != nil {
		return nil, mapBookRepositoryError(err)
	}
	if book.SellerID != sellerID {
		return nil, ErrBookNotOwner
	}
	if !bookImagesMutable(book.Status) {
		return nil, ErrBookStateConflict
	}

	staged, err := s.images.Stage(ctx, source)
	if err != nil {
		return nil, mapImageStorageError(err)
	}
	defer func() {
		if discardErr := s.images.Discard(staged); discardErr != nil {
			s.logger.Warn("failed to discard staged image", "error", discardErr)
		}
	}()

	var (
		createdImage *model.BookImage
		finalURL     string
	)
	err = s.books.WithinTransaction(ctx, func(store repository.BookStore) error {
		lockedBook, err := store.FindByIDForUpdate(ctx, bookID)
		if err != nil {
			return mapBookRepositoryError(err)
		}
		if lockedBook.SellerID != sellerID {
			return ErrBookNotOwner
		}
		if !bookImagesMutable(lockedBook.Status) {
			return ErrBookStateConflict
		}
		images, err := store.ListImages(ctx, bookID)
		if err != nil {
			return err
		}
		if len(images) >= maxBookImages {
			return ErrBookImageLimitReached
		}
		nextSortOrder := uint16(1)
		if len(images) > 0 {
			nextSortOrder = images[len(images)-1].SortOrder + 1
		}
		finalURL, err = s.images.Commit(ctx, staged)
		if err != nil {
			return mapImageStorageError(err)
		}
		createdImage = &model.BookImage{
			BookID:    bookID,
			ImageURL:  finalURL,
			SortOrder: nextSortOrder,
		}
		return store.CreateImage(ctx, createdImage)
	})
	if err != nil {
		if finalURL != "" {
			if deleteErr := s.images.Delete(finalURL); deleteErr != nil {
				s.logger.Warn("failed to compensate committed image", "image_url", finalURL, "error", deleteErr)
			}
		}
		return nil, err
	}
	response := imageResponse(*createdImage)
	return &response, nil
}

func (s *Book) DeleteImage(ctx context.Context, sellerID, bookID, imageID uint64) error {
	var imageURL string
	err := s.books.WithinTransaction(ctx, func(store repository.BookStore) error {
		book, err := store.FindByIDForUpdate(ctx, bookID)
		if err != nil {
			return mapBookRepositoryError(err)
		}
		if book.SellerID != sellerID {
			return ErrBookNotOwner
		}
		if !bookImagesMutable(book.Status) {
			return ErrBookStateConflict
		}
		image, err := store.FindImageByID(ctx, bookID, imageID)
		if err != nil {
			return mapBookRepositoryError(err)
		}
		images, err := store.ListImages(ctx, bookID)
		if err != nil {
			return err
		}
		if book.Status == model.BookStatusAvailable && len(images) == 1 {
			return ErrBookImageRequired
		}
		if err := store.DeleteImage(ctx, imageID); err != nil {
			return err
		}
		remaining := make([]uint64, 0, len(images)-1)
		for _, existing := range images {
			if existing.ID != imageID {
				remaining = append(remaining, existing.ID)
			}
		}
		if len(remaining) > 0 {
			if err := store.ReplaceImageOrder(ctx, bookID, remaining); err != nil {
				return err
			}
		}
		imageURL = image.ImageURL
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.images.Delete(imageURL); err != nil {
		s.logger.Warn("book image metadata deleted but file cleanup failed", "image_url", imageURL, "error", err)
	}
	return nil
}

func (s *Book) ReorderImages(ctx context.Context, sellerID, bookID uint64, imageIDs []uint64) ([]dto.BookImageResponse, error) {
	if len(imageIDs) == 0 || len(imageIDs) > maxBookImages {
		return nil, ErrValidation
	}
	err := s.books.WithinTransaction(ctx, func(store repository.BookStore) error {
		book, err := store.FindByIDForUpdate(ctx, bookID)
		if err != nil {
			return mapBookRepositoryError(err)
		}
		if book.SellerID != sellerID {
			return ErrBookNotOwner
		}
		if !bookImagesMutable(book.Status) {
			return ErrBookStateConflict
		}
		images, err := store.ListImages(ctx, bookID)
		if err != nil {
			return err
		}
		if !sameImageIDs(images, imageIDs) {
			return ErrValidation
		}
		return store.ReplaceImageOrder(ctx, bookID, imageIDs)
	})
	if err != nil {
		return nil, err
	}
	images, err := s.books.ListImages(ctx, bookID)
	if err != nil {
		return nil, err
	}
	responses := make([]dto.BookImageResponse, 0, len(images))
	for _, image := range images {
		responses = append(responses, imageResponse(image))
	}
	return responses, nil
}

func (s *Book) PutOnShelf(ctx context.Context, sellerID, bookID uint64) (*dto.BookResponse, error) {
	return s.changeOwnerStatus(ctx, sellerID, bookID, model.BookStatusOffShelf, model.BookStatusAvailable, true)
}

func (s *Book) TakeOffShelf(ctx context.Context, sellerID, bookID uint64) (*dto.BookResponse, error) {
	return s.changeOwnerStatus(ctx, sellerID, bookID, model.BookStatusAvailable, model.BookStatusOffShelf, false)
}

func (s *Book) changeOwnerStatus(
	ctx context.Context,
	sellerID, bookID uint64,
	from, to model.BookStatus,
	requireImage bool,
) (*dto.BookResponse, error) {
	var changed *model.Book
	err := s.books.WithinTransaction(ctx, func(store repository.BookStore) error {
		book, err := store.FindByIDForUpdate(ctx, bookID)
		if err != nil {
			return mapBookRepositoryError(err)
		}
		if book.SellerID != sellerID {
			return ErrBookNotOwner
		}
		if book.Status != from {
			return ErrBookStateConflict
		}
		if requireImage {
			count, err := store.CountImages(ctx, bookID)
			if err != nil {
				return err
			}
			if count == 0 {
				return ErrBookImageRequired
			}
			if count > maxBookImages {
				return ErrBookImageLimitReached
			}
		}
		if err := store.UpdateStatus(ctx, bookID, to); err != nil {
			return err
		}
		book.Status = to
		changed = book
		return nil
	})
	if err != nil {
		return nil, err
	}
	images, err := s.books.ListImages(ctx, bookID)
	if err != nil {
		return nil, err
	}
	response := bookResponse(changed, images)
	return &response, nil
}

func (s *Book) Block(ctx context.Context, administrator *model.User, bookID uint64) (*dto.BookResponse, error) {
	if administrator == nil || administrator.Role != model.UserRoleAdmin {
		return nil, ErrForbidden
	}
	return s.changeAdminStatus(ctx, bookID, []model.BookStatus{model.BookStatusAvailable, model.BookStatusOffShelf}, model.BookStatusBlocked)
}

func (s *Book) Unblock(ctx context.Context, administrator *model.User, bookID uint64) (*dto.BookResponse, error) {
	if administrator == nil || administrator.Role != model.UserRoleAdmin {
		return nil, ErrForbidden
	}
	return s.changeAdminStatus(ctx, bookID, []model.BookStatus{model.BookStatusBlocked}, model.BookStatusOffShelf)
}

func (s *Book) changeAdminStatus(
	ctx context.Context,
	bookID uint64,
	allowed []model.BookStatus,
	target model.BookStatus,
) (*dto.BookResponse, error) {
	var changed *model.Book
	err := s.books.WithinTransaction(ctx, func(store repository.BookStore) error {
		book, err := store.FindByIDForUpdate(ctx, bookID)
		if err != nil {
			return mapBookRepositoryError(err)
		}
		if !containsBookStatus(allowed, book.Status) {
			return ErrBookStateConflict
		}
		if err := store.UpdateStatus(ctx, bookID, target); err != nil {
			return err
		}
		book.Status = target
		changed = book
		return nil
	})
	if err != nil {
		return nil, err
	}
	images, err := s.books.ListImages(ctx, bookID)
	if err != nil {
		return nil, err
	}
	response := bookResponse(changed, images)
	return &response, nil
}

func validateBookInput(title, description, price, condition string) (string, *string, string, model.BookCondition, error) {
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > maxBookTitleLength {
		return "", nil, "", "", ErrValidation
	}
	normalizedDescription, err := normalizeDescription(description)
	if err != nil {
		return "", nil, "", "", err
	}
	normalizedPrice, err := normalizePrice(price)
	if err != nil {
		return "", nil, "", "", err
	}
	normalizedCondition, ok := parseBookCondition(condition)
	if !ok {
		return "", nil, "", "", ErrValidation
	}
	return title, normalizedDescription, normalizedPrice, normalizedCondition, nil
}

func normalizeDescription(value string) (*string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxBookDescriptionLength {
		return nil, ErrValidation
	}
	if value == "" {
		return nil, nil
	}
	return &value, nil
}

func normalizePrice(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !pricePattern.MatchString(value) {
		return "", ErrValidation
	}
	parts := strings.SplitN(value, ".", 2)
	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return "", ErrValidation
	}
	fraction := uint64(0)
	if len(parts) == 2 {
		fractionText := parts[1]
		if len(fractionText) == 1 {
			fractionText += "0"
		}
		fraction, err = strconv.ParseUint(fractionText, 10, 64)
		if err != nil {
			return "", ErrValidation
		}
	}
	if whole == 0 && fraction == 0 {
		return "", ErrValidation
	}
	return fmt.Sprintf("%d.%02d", whole, fraction), nil
}

func parseBookCondition(value string) (model.BookCondition, bool) {
	condition := model.BookCondition(strings.TrimSpace(value))
	switch condition {
	case model.BookConditionNew, model.BookConditionLikeNew, model.BookConditionGood, model.BookConditionFair:
		return condition, true
	default:
		return "", false
	}
}

func normalizeBookListQuery(query dto.BookListQuery) (dto.BookListQuery, error) {
	query.Keyword = strings.TrimSpace(query.Keyword)
	if utf8.RuneCountInString(query.Keyword) > maxBookKeywordLength {
		return dto.BookListQuery{}, ErrValidation
	}
	if query.Condition != "" {
		condition, ok := parseBookCondition(query.Condition)
		if !ok {
			return dto.BookListQuery{}, ErrValidation
		}
		query.Condition = string(condition)
	}
	if query.Sort == "" {
		query.Sort = "newest"
	}
	if query.Sort != "newest" && query.Sort != "price_asc" && query.Sort != "price_desc" {
		return dto.BookListQuery{}, ErrValidation
	}
	page, pageSize, err := normalizePagination(query.Page, query.PageSize)
	if err != nil {
		return dto.BookListQuery{}, err
	}
	query.Page = page
	query.PageSize = pageSize
	return query, nil
}

func normalizePagination(page, pageSize int) (int, int, error) {
	if page == 0 {
		page = defaultPage
	}
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if page < 1 || pageSize < 1 || pageSize > maxPageSize {
		return 0, 0, ErrValidation
	}
	return page, pageSize, nil
}

func bookDetailsMutable(status model.BookStatus) bool {
	return status == model.BookStatusOffShelf || status == model.BookStatusAvailable
}

func bookImagesMutable(status model.BookStatus) bool {
	return status == model.BookStatusOffShelf || status == model.BookStatusAvailable
}

func sameImageIDs(images []model.BookImage, imageIDs []uint64) bool {
	if len(images) != len(imageIDs) {
		return false
	}
	known := make(map[uint64]struct{}, len(images))
	for _, image := range images {
		known[image.ID] = struct{}{}
	}
	for _, imageID := range imageIDs {
		if _, exists := known[imageID]; !exists {
			return false
		}
		delete(known, imageID)
	}
	return len(known) == 0
}

func containsBookStatus(statuses []model.BookStatus, candidate model.BookStatus) bool {
	for _, status := range statuses {
		if status == candidate {
			return true
		}
	}
	return false
}

func mapBookRepositoryError(err error) error {
	switch {
	case errors.Is(err, repository.ErrBookNotFound):
		return ErrBookNotFound
	case errors.Is(err, repository.ErrBookImageNotFound):
		return ErrBookImageNotFound
	default:
		return err
	}
}

func mapImageStorageError(err error) error {
	switch {
	case errors.Is(err, storage.ErrImageTooLarge):
		return ErrImageTooLarge
	case errors.Is(err, storage.ErrUnsupportedImageType), errors.Is(err, storage.ErrInvalidImage):
		return ErrInvalidImage
	default:
		return err
	}
}

func bookResponse(book *model.Book, images []model.BookImage) dto.BookResponse {
	imageResponses := make([]dto.BookImageResponse, 0, len(images))
	for _, image := range images {
		imageResponses = append(imageResponses, imageResponse(image))
	}
	return dto.BookResponse{
		ID:          book.ID,
		SellerID:    book.SellerID,
		Title:       book.Title,
		Description: book.Description,
		Price:       book.Price,
		Condition:   string(book.Condition),
		Status:      string(book.Status),
		Images:      imageResponses,
		CreatedAt:   book.CreatedAt,
		UpdatedAt:   book.UpdatedAt,
	}
}

func imageResponse(image model.BookImage) dto.BookImageResponse {
	return dto.BookImageResponse{
		ID:        image.ID,
		ImageURL:  image.ImageURL,
		SortOrder: image.SortOrder,
		CreatedAt: image.CreatedAt,
	}
}

func listResponse(records []repository.BookListRecord, page, pageSize int, total int64) *dto.BookListResponse {
	items := make([]dto.BookListItemResponse, 0, len(records))
	for _, record := range records {
		items = append(items, dto.BookListItemResponse{
			ID:            record.ID,
			SellerID:      record.SellerID,
			Title:         record.Title,
			Price:         record.Price,
			Condition:     string(record.Condition),
			Status:        string(record.Status),
			CoverImageURL: record.CoverImageURL,
			CreatedAt:     record.CreatedAt,
			UpdatedAt:     record.UpdatedAt,
		})
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &dto.BookListResponse{
		Items: items,
		Pagination: dto.PaginationResponse{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages,
		},
	}
}
