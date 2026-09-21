package router

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/gxiaopang/bookloop/backend/internal/config"
	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/handler"
	"github.com/gxiaopang/bookloop/backend/internal/middleware"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
	"github.com/gxiaopang/bookloop/backend/internal/service"
)

func TestAuthAPIRegisterLoginAndCurrentUserNeverExposePasswordHash(t *testing.T) {
	engine, _ := newAuthTestEngine(t, 15*time.Minute)

	registerResponse := performJSONRequest(t, engine, http.MethodPost, "/api/v1/auth/register", map[string]any{
		"username": "reader_1",
		"email":    "Reader@Example.COM",
		"password": "password-one",
		"role":     "admin",
		"status":   "disabled",
	})
	if registerResponse.Code != http.StatusCreated {
		t.Fatalf("register status = %d, body = %s", registerResponse.Code, registerResponse.Body.String())
	}
	assertNoPasswordHash(t, registerResponse.Body.String())
	if strings.Contains(registerResponse.Body.String(), `"role":"admin"`) ||
		strings.Contains(registerResponse.Body.String(), `"status":"disabled"`) {
		t.Fatalf("register body honors client-controlled role/status: %s", registerResponse.Body.String())
	}

	loginResponse := performJSONRequest(t, engine, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"identifier": "READER@EXAMPLE.COM",
		"password":   "password-one",
	})
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResponse.Code, loginResponse.Body.String())
	}
	assertNoPasswordHash(t, loginResponse.Body.String())
	token := accessTokenFromResponse(t, loginResponse)

	meResponse := performAuthorizedRequest(engine, token)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("me status = %d, body = %s", meResponse.Code, meResponse.Body.String())
	}
	assertNoPasswordHash(t, meResponse.Body.String())
	if !strings.Contains(meResponse.Body.String(), `"email":"reader@example.com"`) {
		t.Fatalf("me body = %s, want normalized email", meResponse.Body.String())
	}
}

func TestCurrentUserRequiresJWT(t *testing.T) {
	engine, _ := newAuthTestEngine(t, 15*time.Minute)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"UNAUTHORIZED"`) {
		t.Fatalf("status/body = %d/%s, want 401 UNAUTHORIZED", response.Code, response.Body.String())
	}
}

func TestCurrentUserRejectsMalformedBearerHeader(t *testing.T) {
	engine, _ := newAuthTestEngine(t, 15*time.Minute)
	for _, header := range []string{"token", "Basic token", "Bearer", "Bearer one two"} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
		request.Header.Set("Authorization", header)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"UNAUTHORIZED"`) {
			t.Fatalf("header %q status/body = %d/%s, want 401 UNAUTHORIZED", header, response.Code, response.Body.String())
		}
	}
}

func TestCurrentUserRejectsTamperedJWT(t *testing.T) {
	engine, users := newAuthTestEngine(t, 15*time.Minute)
	users.addUser(t, "reader", "reader@example.com", "password-one", model.UserStatusActive)
	token := loginForToken(t, engine, "reader", "password-one")
	lastCharacter := "x"
	if strings.HasSuffix(token, lastCharacter) {
		lastCharacter = "y"
	}
	tampered := token[:len(token)-1] + lastCharacter

	response := performAuthorizedRequest(engine, tampered)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"UNAUTHORIZED"`) {
		t.Fatalf("status/body = %d/%s, want 401 UNAUTHORIZED", response.Code, response.Body.String())
	}
}

func TestCurrentUserRejectsExpiredJWT(t *testing.T) {
	engine, users := newAuthTestEngine(t, -time.Minute)
	users.addUser(t, "reader", "reader@example.com", "password-one", model.UserStatusActive)
	token := loginForToken(t, engine, "reader", "password-one")

	response := performAuthorizedRequest(engine, token)
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"TOKEN_EXPIRED"`) {
		t.Fatalf("status/body = %d/%s, want 401 TOKEN_EXPIRED", response.Code, response.Body.String())
	}
}

func TestDisabledUserCannotUsePreviouslyIssuedJWT(t *testing.T) {
	engine, users := newAuthTestEngine(t, 15*time.Minute)
	user := users.addUser(t, "reader", "reader@example.com", "password-one", model.UserStatusActive)
	token := loginForToken(t, engine, "reader", "password-one")

	users.byID[user.ID].Status = model.UserStatusDisabled
	response := performAuthorizedRequest(engine, token)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"USER_DISABLED"`) {
		t.Fatalf("status/body = %d/%s, want 403 USER_DISABLED", response.Code, response.Body.String())
	}
}

func TestBookMutationRoutesRequireAuthentication(t *testing.T) {
	engine, _ := newAuthTestEngine(t, 15*time.Minute)
	response := performJSONRequest(t, engine, http.MethodPost, "/api/v1/books", map[string]any{
		"title": "Book", "price": "10.00", "condition": "good",
	})
	if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), `"code":"UNAUTHORIZED"`) {
		t.Fatalf("status/body = %d/%s, want 401 UNAUTHORIZED", response.Code, response.Body.String())
	}
}

func TestAdminBookRouteUsesCurrentDatabaseRole(t *testing.T) {
	engine, users := newAuthTestEngine(t, 15*time.Minute)
	user := users.addUser(t, "administrator", "admin@example.com", "password-one", model.UserStatusActive)
	user.Role = model.UserRoleAdmin
	token := loginForToken(t, engine, "administrator", "password-one")

	// Simulate a role downgrade after the token was issued. Authorization must
	// use the current database-backed user, not any stale token information.
	users.byID[user.ID].Role = model.UserRoleUser
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/books/1/block", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":"FORBIDDEN"`) {
		t.Fatalf("status/body = %d/%s, want 403 FORBIDDEN", response.Code, response.Body.String())
	}
}

func newAuthTestEngine(t *testing.T, tokenLifetime time.Duration) (*gin.Engine, *apiUserRepository) {
	t.Helper()
	logger := config.NewLogger(io.Discard, "debug")
	users := &apiUserRepository{byID: make(map[uint64]*model.User), nextID: 1}
	authService, err := service.NewAuth(
		users,
		service.NewJWTManager("test-secret-with-enough-entropy", "bookloop-test", tokenLifetime),
		bcrypt.MinCost,
	)
	if err != nil {
		t.Fatalf("NewAuth() error = %v", err)
	}
	readiness := service.NewReadiness(
		func(context.Context) error { return nil },
		func(context.Context) error { return nil },
		time.Second,
		false,
	)
	return New(
		logger,
		handler.NewHealth(readiness, logger),
		handler.NewAuth(authService, logger),
		handler.NewBook(nil, logger, 1024),
		handler.NewFavorite(nil, logger),
		handler.NewOrder(nil, logger),
		middleware.NewAuthentication(authService, logger),
	), users
}

func performJSONRequest(
	t *testing.T,
	engine *gin.Engine,
	method string,
	path string,
	body any,
) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func loginForToken(t *testing.T, engine *gin.Engine, identifier, password string) string {
	t.Helper()
	response := performJSONRequest(t, engine, http.MethodPost, "/api/v1/auth/login", map[string]any{
		"identifier": identifier,
		"password":   password,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}
	return accessTokenFromResponse(t, response)
}

func accessTokenFromResponse(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Data dto.LoginResponse `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if body.Data.AccessToken == "" {
		t.Fatalf("login body has empty access token: %s", response.Body.String())
	}
	return body.Data.AccessToken
}

func performAuthorizedRequest(engine *gin.Engine, token string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func assertNoPasswordHash(t *testing.T, body string) {
	t.Helper()
	if strings.Contains(body, "password_hash") || strings.Contains(body, "$2a$") ||
		strings.Contains(body, "$2b$") || strings.Contains(body, "password-one") {
		t.Fatalf("response exposes password hash: %s", body)
	}
}

type apiUserRepository struct {
	byID   map[uint64]*model.User
	nextID uint64
}

func (r *apiUserRepository) Create(_ context.Context, user *model.User) error {
	for _, existing := range r.byID {
		if strings.EqualFold(existing.Username, user.Username) {
			return repository.ErrUsernameAlreadyExists
		}
		if strings.EqualFold(existing.Email, user.Email) {
			return repository.ErrEmailAlreadyExists
		}
	}
	user.ID = r.nextID
	r.nextID++
	user.CreatedAt = time.Now().UTC()
	user.UpdatedAt = user.CreatedAt
	r.byID[user.ID] = user
	return nil
}

func (r *apiUserRepository) FindByID(_ context.Context, id uint64) (*model.User, error) {
	user, ok := r.byID[id]
	if !ok {
		return nil, repository.ErrUserNotFound
	}
	return user, nil
}

func (r *apiUserRepository) FindByUsername(_ context.Context, username string) (*model.User, error) {
	for _, user := range r.byID {
		if strings.EqualFold(user.Username, username) {
			return user, nil
		}
	}
	return nil, repository.ErrUserNotFound
}

func (r *apiUserRepository) FindByEmail(_ context.Context, email string) (*model.User, error) {
	for _, user := range r.byID {
		if strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	return nil, repository.ErrUserNotFound
}

func (r *apiUserRepository) addUser(
	t *testing.T,
	username string,
	email string,
	password string,
	status model.UserStatus,
) *model.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	user := &model.User{
		Username:     username,
		Email:        strings.ToLower(email),
		PasswordHash: string(hash),
		Role:         model.UserRoleUser,
		Status:       status,
	}
	if err := r.Create(context.Background(), user); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return user
}
