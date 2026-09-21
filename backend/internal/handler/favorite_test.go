package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

func TestFavoriteHandlerUsesAuthenticatedUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	favorites := &handlerFavoriteRepository{}
	books := &handlerFavoriteBookRepository{book: &model.Book{ID: 7}}
	logger := config.NewLogger(io.Discard, "debug")
	handler := NewFavorite(service.NewFavorite(favorites, books), logger)

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPut, "/api/v1/favorites/7", nil)
	context.Params = gin.Params{{Key: "book_id", Value: "7"}}
	context.Set("current_user", &model.User{ID: 42})
	handler.Add(context)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"favorited":true`) {
		t.Fatalf("status/body = %d/%s, want 200 favorited", response.Code, response.Body.String())
	}
	if favorites.added == nil || favorites.added.UserID != 42 || favorites.added.BookID != 7 {
		t.Fatalf("added favorite = %+v, want authenticated user 42 and book 7", favorites.added)
	}
}

func TestFavoriteHandlerValidatesListQueryAndBookID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := config.NewLogger(io.Discard, "debug")
	handler := NewFavorite(service.NewFavorite(&handlerFavoriteRepository{}, &handlerFavoriteBookRepository{}), logger)

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/favorites?status=available", nil)
	context.Set("current_user", &model.User{ID: 42})
	handler.List(context)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"VALIDATION_ERROR"`) {
		t.Fatalf("unknown query status/body = %d/%s, want 400 validation error", response.Code, response.Body.String())
	}

	response = httptest.NewRecorder()
	context, _ = gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/favorites/invalid", nil)
	context.Params = gin.Params{{Key: "book_id", Value: "invalid"}}
	context.Set("current_user", &model.User{ID: 42})
	handler.Remove(context)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid ID status = %d, want 400", response.Code)
	}
}

type handlerFavoriteRepository struct {
	added *model.Favorite
}

func (r *handlerFavoriteRepository) Add(_ context.Context, favorite *model.Favorite) error {
	copy := *favorite
	r.added = &copy
	return nil
}

func (r *handlerFavoriteRepository) Remove(context.Context, uint64, uint64) error {
	return nil
}

func (r *handlerFavoriteRepository) ListByUser(context.Context, uint64, int, int) ([]repository.FavoriteListRecord, int64, error) {
	return nil, 0, nil
}

type handlerFavoriteBookRepository struct {
	book *model.Book
}

func (r *handlerFavoriteBookRepository) FindByID(context.Context, uint64) (*model.Book, error) {
	if r.book == nil {
		return nil, repository.ErrBookNotFound
	}
	return r.book, nil
}
