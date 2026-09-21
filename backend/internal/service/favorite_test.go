package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
)

func TestFavoriteAddValidatesBookAndUsesCurrentUser(t *testing.T) {
	favorites := &fakeFavoriteRepository{}
	books := &fakeFavoriteBookRepository{books: map[uint64]*model.Book{
		7: {ID: 7, Status: model.BookStatusOffShelf},
	}}
	service := NewFavorite(favorites, books)

	if err := service.Add(context.Background(), 42, 7); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if favorites.added == nil || favorites.added.UserID != 42 || favorites.added.BookID != 7 {
		t.Fatalf("added favorite = %+v, want current user 42 and book 7", favorites.added)
	}
	if err := service.Add(context.Background(), 42, 999); !errors.Is(err, ErrBookNotFound) {
		t.Fatalf("Add(missing book) error = %v, want ErrBookNotFound", err)
	}
}

func TestFavoriteRemoveIsScopedAndIdempotent(t *testing.T) {
	favorites := &fakeFavoriteRepository{}
	service := NewFavorite(favorites, &fakeFavoriteBookRepository{})

	for range 2 {
		if err := service.Remove(context.Background(), 42, 7); err != nil {
			t.Fatalf("Remove() error = %v", err)
		}
	}
	if len(favorites.removed) != 2 {
		t.Fatalf("remove calls = %+v, want two successful idempotent calls", favorites.removed)
	}
	for _, removed := range favorites.removed {
		if removed.userID != 42 || removed.bookID != 7 {
			t.Fatalf("remove scope = %+v, want current user 42 and book 7", removed)
		}
	}
}

func TestFavoriteListKeepsUnavailableBooksVisible(t *testing.T) {
	now := time.Now().UTC()
	favorites := &fakeFavoriteRepository{records: []repository.FavoriteListRecord{
		{BookID: 1, Status: model.BookStatusOffShelf, IsAvailable: false, FavoritedAt: now},
		{BookID: 2, Status: model.BookStatusBlocked, IsAvailable: false, FavoritedAt: now},
		{BookID: 3, Status: model.BookStatusSold, IsAvailable: false, FavoritedAt: now},
		{BookID: 4, Status: model.BookStatusAvailable, IsAvailable: true, FavoritedAt: now},
	}}
	service := NewFavorite(favorites, &fakeFavoriteBookRepository{})

	response, err := service.List(context.Background(), 42, 0, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if favorites.listedUserID != 42 {
		t.Fatalf("listed user = %d, want current user 42", favorites.listedUserID)
	}
	if response.Pagination.Page != 1 || response.Pagination.PageSize != 20 || response.Pagination.Total != 4 {
		t.Fatalf("pagination = %+v, want normalized page with four records", response.Pagination)
	}
	if len(response.Items) != 4 {
		t.Fatalf("items = %+v, want all favorite statuses", response.Items)
	}
	for index := 0; index < 3; index++ {
		if response.Items[index].IsAvailable {
			t.Fatalf("item %d is_available = true, want false for status %q", index, response.Items[index].Status)
		}
	}
	if !response.Items[3].IsAvailable {
		t.Fatal("available item is_available = false, want true")
	}
}

type fakeFavoriteRepository struct {
	added        *model.Favorite
	removed      []favoriteRemoval
	records      []repository.FavoriteListRecord
	listedUserID uint64
}

type favoriteRemoval struct {
	userID uint64
	bookID uint64
}

func (r *fakeFavoriteRepository) Add(_ context.Context, favorite *model.Favorite) error {
	copy := *favorite
	r.added = &copy
	return nil
}

func (r *fakeFavoriteRepository) Remove(_ context.Context, userID, bookID uint64) error {
	r.removed = append(r.removed, favoriteRemoval{userID: userID, bookID: bookID})
	return nil
}

func (r *fakeFavoriteRepository) ListByUser(_ context.Context, userID uint64, _, _ int) ([]repository.FavoriteListRecord, int64, error) {
	r.listedUserID = userID
	return r.records, int64(len(r.records)), nil
}

type fakeFavoriteBookRepository struct {
	books map[uint64]*model.Book
}

func (r *fakeFavoriteBookRepository) FindByID(_ context.Context, bookID uint64) (*model.Book, error) {
	book, ok := r.books[bookID]
	if !ok {
		return nil, repository.ErrBookNotFound
	}
	return book, nil
}
