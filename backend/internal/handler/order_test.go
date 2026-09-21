package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

func TestOrderHandlerCreateReturnsCreatedThenReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := &handlerOrderRepository{
		book:   &model.Book{ID: 7, SellerID: 10, Title: "Book", Price: "12.00", Status: model.BookStatusAvailable},
		seller: &model.User{ID: 10, Status: model.UserStatusActive},
		now:    time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
	}
	logger := config.NewLogger(io.Discard, "debug")
	handler := NewOrder(service.NewOrder(repository, nil, 15*time.Minute, service.OrderIdempotencyOptions{}, logger), logger)

	for attempt, expectedStatus := range []int{http.StatusCreated, http.StatusOK} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(`{"book_id":7}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Idempotency-Key", "purchase-intent")
		context, _ := gin.CreateTestContext(response)
		context.Request = request
		context.Set("current_user", &model.User{ID: 20})
		handler.Create(context)

		if response.Code != expectedStatus || !strings.Contains(response.Body.String(), `"status":"pending_payment"`) {
			t.Fatalf("attempt %d status/body = %d/%s, want %d pending order", attempt+1, response.Code, response.Body.String(), expectedStatus)
		}
		if strings.Contains(response.Body.String(), "idempotency_key") || strings.Contains(response.Body.String(), "request_fingerprint") {
			t.Fatalf("response exposes internal idempotency data: %s", response.Body.String())
		}
	}
}

func TestOrderHandlerValidatesHeaderQueryAndOrderNumber(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := config.NewLogger(io.Discard, "debug")
	repository := &handlerOrderRepository{
		book:   &model.Book{ID: 7, SellerID: 10, Status: model.BookStatusAvailable},
		seller: &model.User{ID: 10, Status: model.UserStatusActive},
		now:    time.Now().UTC(),
	}
	handler := NewOrder(service.NewOrder(repository, nil, 15*time.Minute, service.OrderIdempotencyOptions{}, logger), logger)

	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(`{"book_id":7}`))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Set("current_user", &model.User{ID: 20})
	handler.Create(context)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("missing idempotency key status = %d, want 400", response.Code)
	}

	response = httptest.NewRecorder()
	context, _ = gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/orders?status=paid", nil)
	context.Set("current_user", &model.User{ID: 20})
	handler.List(context)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown list query status = %d, want 400", response.Code)
	}

	for _, orderNo := range []string{"short", "01HF7YAT00IIIIIIIIIIIIIIII", "01HF7YAT00ABCDEFGHJKMNPQRS"} {
		context, _ = gin.CreateTestContext(httptest.NewRecorder())
		context.Params = gin.Params{{Key: "order_no", Value: orderNo}}
		_, ok := parseOrderNo(context)
		want := orderNo == "01HF7YAT00ABCDEFGHJKMNPQRS"
		if ok != want {
			t.Fatalf("parseOrderNo(%q) ok = %v, want %v", orderNo, ok, want)
		}
	}
}

func TestOrderHandlerRequestInProgressIncludesRetryAfter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	response := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(response)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil)
	writeServiceError(context, config.NewLogger(io.Discard, "debug"), service.ErrRequestInProgress)

	if response.Code != http.StatusConflict || response.Header().Get("Retry-After") != "1" ||
		!strings.Contains(response.Body.String(), `"code":"REQUEST_IN_PROGRESS"`) {
		t.Fatalf("status/header/body = %d/%q/%s, want 409 Retry-After=1 REQUEST_IN_PROGRESS",
			response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
}

type handlerOrderRepository struct {
	book   *model.Book
	seller *model.User
	order  *model.Order
	now    time.Time
}

func (r *handlerOrderRepository) WithinTransaction(_ context.Context, operation func(repository.OrderStore) error) error {
	return operation(r)
}

func (r *handlerOrderRepository) CurrentTime(context.Context) (time.Time, error) {
	return r.now, nil
}

func (r *handlerOrderRepository) FindByBuyerAndIdempotencyKey(_ context.Context, buyerID uint64, key string) (*model.Order, error) {
	if r.order != nil && r.order.BuyerID == buyerID && r.order.IdempotencyKey == key {
		return r.order, nil
	}
	return nil, repository.ErrOrderNotFound
}

func (r *handlerOrderRepository) FindByBuyerAndIdempotencyKeyForUpdate(ctx context.Context, buyerID uint64, key string) (*model.Order, error) {
	return r.FindByBuyerAndIdempotencyKey(ctx, buyerID, key)
}

func (r *handlerOrderRepository) FindBookByIDForUpdate(context.Context, uint64) (*model.Book, error) {
	return r.book, nil
}

func (r *handlerOrderRepository) FindPendingByBookForUpdate(context.Context, uint64) (*model.Order, error) {
	if r.order != nil && r.order.Status == model.OrderStatusPendingPayment {
		return r.order, nil
	}
	return nil, repository.ErrOrderNotFound
}

func (r *handlerOrderRepository) FindUserByIDForShare(context.Context, uint64) (*model.User, error) {
	return r.seller, nil
}

func (r *handlerOrderRepository) FindByOrderNo(context.Context, string) (*model.Order, error) {
	if r.order == nil {
		return nil, repository.ErrOrderNotFound
	}
	return r.order, nil
}

func (r *handlerOrderRepository) FindByOrderNoForUpdate(ctx context.Context, orderNo string) (*model.Order, error) {
	return r.FindByOrderNo(ctx, orderNo)
}

func (r *handlerOrderRepository) Create(_ context.Context, order *model.Order) error {
	order.ID = 1
	order.CreatedAt = r.now
	order.UpdatedAt = r.now
	r.order = order
	return nil
}

func (r *handlerOrderRepository) UpdateBookStatus(_ context.Context, _ uint64, from, to model.BookStatus) error {
	if r.book.Status != from {
		return repository.ErrConcurrentUpdate
	}
	r.book.Status = to
	return nil
}

func (r *handlerOrderRepository) UpdateOrderStatus(context.Context, uint64, model.OrderStatus, model.OrderStatus, map[string]any) error {
	return nil
}

func (r *handlerOrderRepository) ListByBuyer(context.Context, uint64, int, int) ([]model.Order, int64, error) {
	return nil, 0, nil
}

func (r *handlerOrderRepository) ListBySeller(context.Context, uint64, int, int) ([]model.Order, int64, error) {
	return nil, 0, nil
}

func (r *handlerOrderRepository) ListAll(context.Context, int, int) ([]model.Order, int64, error) {
	return nil, 0, nil
}

func (r *handlerOrderRepository) FindExpiredPendingOrderNos(context.Context, int) ([]string, error) {
	return nil, nil
}
