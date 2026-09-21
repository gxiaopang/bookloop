package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

func TestParseBookListQueryAcceptsOnlyDocumentedParameters(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodGet,
		"/api/v1/books?keyword=Go&condition=good&sort=price_asc&page=2&page_size=10", nil)
	query, err := parseBookListQuery(context)
	if err != nil {
		t.Fatalf("parseBookListQuery() error = %v", err)
	}
	if query.Keyword != "Go" || query.Condition != "good" || query.Sort != "price_asc" || query.Page != 2 || query.PageSize != 10 {
		t.Fatalf("query = %+v, want parsed documented values", query)
	}

	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/books?seller_id=1", nil)
	if _, err := parseBookListQuery(context); err == nil {
		t.Fatal("parseBookListQuery(unknown parameter) error = nil, want validation error")
	}
}

func TestBookHandlerMapsDomainErrorsWithoutInternalDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger := config.NewLogger(io.Discard, "debug")
	tests := []struct {
		err        error
		statusCode int
		code       string
	}{
		{err: service.ErrBookNotOwner, statusCode: http.StatusForbidden, code: "BOOK_NOT_OWNER"},
		{err: service.ErrBookNotFound, statusCode: http.StatusNotFound, code: "BOOK_NOT_FOUND"},
		{err: service.ErrBookStateConflict, statusCode: http.StatusConflict, code: "BOOK_STATE_CONFLICT"},
		{err: service.ErrBookImageRequired, statusCode: http.StatusConflict, code: "BOOK_IMAGE_REQUIRED"},
		{err: service.ErrImageTooLarge, statusCode: http.StatusRequestEntityTooLarge, code: "IMAGE_TOO_LARGE"},
		{err: service.ErrInvalidImage, statusCode: http.StatusUnprocessableEntity, code: "INVALID_IMAGE"},
		{err: service.ErrBookNotAvailable, statusCode: http.StatusConflict, code: "BOOK_NOT_AVAILABLE"},
		{err: service.ErrCannotPurchaseOwnBook, statusCode: http.StatusForbidden, code: "CANNOT_PURCHASE_OWN_BOOK"},
		{err: service.ErrOrderNotFound, statusCode: http.StatusNotFound, code: "ORDER_NOT_FOUND"},
		{err: service.ErrOrderAccessDenied, statusCode: http.StatusForbidden, code: "ORDER_ACCESS_DENIED"},
		{err: service.ErrOrderStateConflict, statusCode: http.StatusConflict, code: "ORDER_STATE_CONFLICT"},
		{err: service.ErrIdempotencyKeyReused, statusCode: http.StatusConflict, code: "IDEMPOTENCY_KEY_REUSED"},
		{err: service.ErrRequestInProgress, statusCode: http.StatusConflict, code: "REQUEST_IN_PROGRESS"},
		{err: service.ErrDependencyUnavailable, statusCode: http.StatusServiceUnavailable, code: "SERVICE_UNAVAILABLE"},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			context.Set("request_id", "test-request")
			writeServiceError(context, logger, test.err)
			if response.Code != test.statusCode || !strings.Contains(response.Body.String(), `"code":"`+test.code+`"`) {
				t.Fatalf("status/body = %d/%s, want %d/%s", response.Code, response.Body.String(), test.statusCode, test.code)
			}
			if strings.Contains(response.Body.String(), "SQL") || strings.Contains(response.Body.String(), "password_hash") {
				t.Fatalf("response exposes internal details: %s", response.Body.String())
			}
		})
	}
}

func TestBookIDParsingRejectsInvalidValues(t *testing.T) {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, value := range []string{"", "0", "-1", "abc"} {
		context.Params = gin.Params{{Key: "book_id", Value: value}}
		if _, ok := parseID(context, "book_id"); ok {
			t.Fatalf("parseID(%q) ok = true, want false", value)
		}
	}
	context.Params = gin.Params{{Key: "book_id", Value: "42"}}
	if id, ok := parseID(context, "book_id"); !ok || id != 42 {
		t.Fatalf("parseID(42) = %d/%v, want 42/true", id, ok)
	}
}
