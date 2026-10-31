package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/handler"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

func TestHealthzDoesNotCheckDependencies(t *testing.T) {
	engine := testEngine(t, io.Discard,
		func(context.Context) error { return errors.New("mysql unavailable") },
		func(context.Context) error { return errors.New("redis unavailable") },
	)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	assertResponseHasRequestID(t, response)
}

func TestReadyzAllowsRedisDegradation(t *testing.T) {
	engine := testEngine(t, io.Discard,
		func(context.Context) error { return nil },
		func(context.Context) error { return errors.New("redis unavailable") },
	)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), `"status":"degraded"`) {
		t.Fatalf("body = %s, want degraded status", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"redis":{"required":false,"status":"down"}`) {
		t.Fatalf("body = %s, want Redis down status", response.Body.String())
	}
}

func TestReadyzRequiresRedisWhenDegradationIsDisabled(t *testing.T) {
	engine := testEngine(t, io.Discard,
		func(context.Context) error { return nil },
		func(context.Context) error { return errors.New("redis unavailable") },
		true,
	)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(response.Body.String(), `"redis":{"required":true,"status":"down"}`) {
		t.Fatalf("body = %s, want required Redis down status", response.Body.String())
	}
}

func TestReadyzFailsWhenMySQLIsUnavailable(t *testing.T) {
	engine := testEngine(t, io.Discard,
		func(context.Context) error { return errors.New("mysql unavailable") },
		func(context.Context) error { return nil },
	)

	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(response.Body.String(), `"code":"SERVICE_UNAVAILABLE"`) {
		t.Fatalf("body = %s, want SERVICE_UNAVAILABLE", response.Body.String())
	}
}

func TestRequestIDAcceptsValidatedHeader(t *testing.T) {
	engine := testEngine(t, io.Discard,
		func(context.Context) error { return nil },
		func(context.Context) error { return nil },
	)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "gateway-request-123")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != "gateway-request-123" {
		t.Fatalf("X-Request-ID = %q, want gateway-request-123", got)
	}

	var body dto.Response
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.RequestID != "gateway-request-123" {
		t.Fatalf("request_id = %q, want gateway-request-123", body.RequestID)
	}
}

func TestRequestIDReplacesInvalidHeader(t *testing.T) {
	engine := testEngine(t, io.Discard,
		func(context.Context) error { return nil },
		func(context.Context) error { return nil },
	)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "invalid request id")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	got := response.Header().Get("X-Request-ID")
	if got == "" || got == "invalid request id" {
		t.Fatalf("X-Request-ID = %q, want generated ID", got)
	}
}

func TestRecoveryUsesUnifiedErrorResponse(t *testing.T) {
	engine := testEngine(t, io.Discard,
		func(context.Context) error { return nil },
		func(context.Context) error { return nil },
	)
	engine.GET("/panic", func(*gin.Context) { panic("test panic") })

	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(response.Body.String(), `"code":"INTERNAL_ERROR"`) {
		t.Fatalf("body = %s, want INTERNAL_ERROR", response.Body.String())
	}
	assertResponseHasRequestID(t, response)
}

func TestAccessLogContainsRequiredFields(t *testing.T) {
	var logOutput bytes.Buffer
	engine := testEngine(t, &logOutput,
		func(context.Context) error { return nil },
		func(context.Context) error { return nil },
	)

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	logLine := logOutput.String()
	for _, field := range []string{`"request_id"`, `"method":"GET"`, `"path":"/healthz"`, `"status":200`, `"duration"`} {
		if !strings.Contains(logLine, field) {
			t.Fatalf("log = %s, want field %s", logLine, field)
		}
	}
}

func TestFavoriteRoutesRequireAuthentication(t *testing.T) {
	engine := testEngine(t, io.Discard,
		func(context.Context) error { return nil },
		func(context.Context) error { return nil },
	)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/v1/favorites", nil),
		httptest.NewRequest(http.MethodPut, "/api/v1/favorites/1", nil),
		httptest.NewRequest(http.MethodDelete, "/api/v1/favorites/1", nil),
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"UNAUTHORIZED"`) {
			t.Fatalf("%s %s status/body = %d/%s, want 401 UNAUTHORIZED",
				request.Method, request.URL.Path, response.Code, response.Body.String())
		}
	}
}

func TestOrderRoutesRequireAuthentication(t *testing.T) {
	engine := testEngine(t, io.Discard,
		func(context.Context) error { return nil },
		func(context.Context) error { return nil },
	)

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/orders", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/orders?as=buyer", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/orders/01HF7YAT00ABCDEFGHJKMNPQRS", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/orders/01HF7YAT00ABCDEFGHJKMNPQRS/deliver", nil),
		httptest.NewRequest(http.MethodPost, "/api/v1/orders/01HF7YAT00ABCDEFGHJKMNPQRS/complete", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/notifications", nil),
		httptest.NewRequest(http.MethodPut, "/api/v1/notifications/read-all", nil),
		httptest.NewRequest(http.MethodPut, "/api/v1/notifications/1/read", nil),
		httptest.NewRequest(http.MethodGet, "/api/v1/admin/messaging/status", nil),
	} {
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"UNAUTHORIZED"`) {
			t.Fatalf("%s %s status/body = %d/%s, want 401 UNAUTHORIZED",
				request.Method, request.URL.Path, response.Code, response.Body.String())
		}
	}
}

func TestPaymentRouteIsRemoved(t *testing.T) {
	engine := testEngine(t, io.Discard, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/orders/01HF7YAT00ABCDEFGHJKMNPQRS/pay", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("payment route status = %d, want 404", response.Code)
	}
}

func TestWechatContactUploadRequiresAuthentication(t *testing.T) {
	engine := testEngine(t, io.Discard, func(context.Context) error { return nil }, func(context.Context) error { return nil })
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/books/1/wechat-qr-code", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("upload status = %d, want 401", response.Code)
	}
}

func testEngine(
	t *testing.T,
	logOutput io.Writer,
	mysqlPing func(context.Context) error,
	redisPing func(context.Context) error,
	redisRequired ...bool,
) *gin.Engine {
	t.Helper()
	logger := config.NewLogger(logOutput, "debug")
	required := false
	if len(redisRequired) > 0 {
		required = redisRequired[0]
	}
	readiness := service.NewReadiness(mysqlPing, redisPing, time.Second, required)
	return New(
		logger,
		handler.NewHealth(readiness, logger),
		handler.NewAuth(nil, logger),
		handler.NewBook(nil, logger, 1024),
		handler.NewFavorite(nil, logger),
		handler.NewOrder(nil, logger),
		handler.NewNotification(nil, logger),
		handler.NewMessagingStatus(nil, logger),
		middleware.NewAuthentication(nil, logger),
	)
}

func assertResponseHasRequestID(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	headerRequestID := response.Header().Get("X-Request-ID")
	if headerRequestID == "" {
		t.Fatal("X-Request-ID header is empty")
	}

	var body dto.Response
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.RequestID != headerRequestID {
		t.Fatalf("body request_id = %q, header = %q", body.RequestID, headerRequestID)
	}
}
