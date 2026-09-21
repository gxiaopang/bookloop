package service

import (
	"context"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
)

type favoriteRepository interface {
	Add(context.Context, *model.Favorite) error
	Remove(context.Context, uint64, uint64) error
	ListByUser(context.Context, uint64, int, int) ([]repository.FavoriteListRecord, int64, error)
}

type favoriteBookRepository interface {
	FindByID(context.Context, uint64) (*model.Book, error)
}

type Favorite struct {
	favorites favoriteRepository
	books     favoriteBookRepository
}

func NewFavorite(favorites favoriteRepository, books favoriteBookRepository) *Favorite {
	return &Favorite{favorites: favorites, books: books}
}

func (s *Favorite) Add(ctx context.Context, userID, bookID uint64) error {
	if _, err := s.books.FindByID(ctx, bookID); err != nil {
		return mapBookRepositoryError(err)
	}
	return s.favorites.Add(ctx, &model.Favorite{UserID: userID, BookID: bookID})
}

func (s *Favorite) Remove(ctx context.Context, userID, bookID uint64) error {
	return s.favorites.Remove(ctx, userID, bookID)
}

func (s *Favorite) List(ctx context.Context, userID uint64, page, pageSize int) (*dto.FavoriteListResponse, error) {
	page, pageSize, err := normalizePagination(page, pageSize)
	if err != nil {
		return nil, err
	}
	records, total, err := s.favorites.ListByUser(ctx, userID, page, pageSize)
	if err != nil {
		return nil, err
	}
	items := make([]dto.FavoriteListItemResponse, 0, len(records))
	for _, record := range records {
		items = append(items, dto.FavoriteListItemResponse{
			BookID:        record.BookID,
			SellerID:      record.SellerID,
			Title:         record.Title,
			Price:         record.Price,
			Condition:     string(record.Condition),
			Status:        string(record.Status),
			IsAvailable:   record.IsAvailable,
			CoverImageURL: record.CoverImageURL,
			BookCreatedAt: record.BookCreatedAt,
			BookUpdatedAt: record.BookUpdatedAt,
			FavoritedAt:   record.FavoritedAt,
		})
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &dto.FavoriteListResponse{
		Items: items,
		Pagination: dto.PaginationResponse{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}
