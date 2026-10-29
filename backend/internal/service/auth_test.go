package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
)

func TestRegisterCreatesActiveUserWithHashedPasswordAndNormalizedEmail(t *testing.T) {
	users := newFakeUserRepository()
	auth := newTestAuth(t, users)

	user, err := auth.Register(context.Background(), dto.RegisterRequest{
		EmailCode: "123456",
		Username:  "reader_1",
		Email:     " Reader@Example.COM ",
		Password:  "correct-horse",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if user.Email != "reader@example.com" {
		t.Fatalf("Email = %q, want lowercase normalized email", user.Email)
	}
	if user.Role != model.UserRoleUser || user.Status != model.UserStatusActive {
		t.Fatalf("role/status = %q/%q, want user/active", user.Role, user.Status)
	}
	if user.PasswordHash == "correct-horse" || !strings.HasPrefix(user.PasswordHash, "$2") {
		t.Fatalf("PasswordHash = %q, want bcrypt hash", user.PasswordHash)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("correct-horse")); err != nil {
		t.Fatalf("CompareHashAndPassword() error = %v", err)
	}
}

func TestRegisterRejectsDuplicateUsername(t *testing.T) {
	users := newFakeUserRepository()
	users.addUser(t, "reader", "first@example.com", "password-one", model.UserStatusActive)
	auth := newTestAuth(t, users)

	_, err := auth.Register(context.Background(), dto.RegisterRequest{
		EmailCode: "123456",
		Username:  "reader",
		Email:     "second@example.com",
		Password:  "password-two",
	})
	if !errors.Is(err, ErrUsernameAlreadyExists) {
		t.Fatalf("Register() error = %v, want ErrUsernameAlreadyExists", err)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	users := newFakeUserRepository()
	users.addUser(t, "reader_one", "reader@example.com", "password-one", model.UserStatusActive)
	auth := newTestAuth(t, users)

	_, err := auth.Register(context.Background(), dto.RegisterRequest{
		EmailCode: "123456",
		Username:  "reader_two",
		Email:     "READER@EXAMPLE.COM",
		Password:  "password-two",
	})
	if !errors.Is(err, ErrEmailAlreadyExists) {
		t.Fatalf("Register() error = %v, want ErrEmailAlreadyExists", err)
	}
}

func TestRegisterMapsUniqueConflictFromCreate(t *testing.T) {
	users := newFakeUserRepository()
	users.createErr = repository.ErrUsernameAlreadyExists
	auth := newTestAuth(t, users)

	_, err := auth.Register(context.Background(), dto.RegisterRequest{
		EmailCode: "123456",
		Username:  "racing_user",
		Email:     "race@example.com",
		Password:  "password-one",
	})
	if !errors.Is(err, ErrUsernameAlreadyExists) {
		t.Fatalf("Register() error = %v, want create-time unique conflict mapping", err)
	}
}

func TestRegisterValidatesUsernameEmailAndPassword(t *testing.T) {
	auth := newTestAuth(t, newFakeUserRepository())
	tests := []dto.RegisterRequest{
		{EmailCode: "123456", Username: "bad name", Email: "reader@example.com", Password: "password-one"},
		{EmailCode: "123456", Username: "reader", Email: "not-an-email", Password: "password-one"},
		{EmailCode: "123456", Username: "reader", Email: "reader@example.com", Password: "short"},
	}
	for _, request := range tests {
		if _, err := auth.Register(context.Background(), request); !errors.Is(err, ErrValidation) {
			t.Fatalf("Register(%+v) error = %v, want ErrValidation", request, err)
		}
	}
}

func TestLoginSucceedsWithUsernameAndEmail(t *testing.T) {
	users := newFakeUserRepository()
	user := users.addUser(t, "reader", "reader@example.com", "password-one", model.UserStatusActive)
	auth := newTestAuth(t, users)

	for _, identifier := range []string{"reader", "READER@EXAMPLE.COM"} {
		result, err := auth.Login(context.Background(), dto.LoginRequest{
			Identifier: identifier,
			Password:   "password-one",
		})
		if err != nil {
			t.Fatalf("Login(%q) error = %v", identifier, err)
		}
		if result.User.ID != user.ID || result.AccessToken == "" {
			t.Fatalf("Login(%q) result = %+v, want user and token", identifier, result)
		}
	}
}

func TestLoginDoesNotRevealWhetherAccountExists(t *testing.T) {
	users := newFakeUserRepository()
	users.addUser(t, "reader", "reader@example.com", "password-one", model.UserStatusActive)
	auth := newTestAuth(t, users)

	requests := []dto.LoginRequest{
		{Identifier: "reader", Password: "wrong-password"},
		{Identifier: "missing", Password: "wrong-password"},
	}
	for _, request := range requests {
		_, err := auth.Login(context.Background(), request)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Login(%q) error = %v, want ErrInvalidCredentials", request.Identifier, err)
		}
	}
}

func TestDisabledUserCannotLogin(t *testing.T) {
	users := newFakeUserRepository()
	users.addUser(t, "reader", "reader@example.com", "password-one", model.UserStatusDisabled)
	auth := newTestAuth(t, users)

	_, err := auth.Login(context.Background(), dto.LoginRequest{
		Identifier: "reader",
		Password:   "password-one",
	})
	if !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("Login() error = %v, want ErrUserDisabled", err)
	}
}

func TestAuthenticateReadsCurrentDatabaseStatus(t *testing.T) {
	users := newFakeUserRepository()
	user := users.addUser(t, "reader", "reader@example.com", "password-one", model.UserStatusActive)
	auth := newTestAuth(t, users)
	token, err := auth.tokens.Issue(user.ID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	users.byID[user.ID].Status = model.UserStatusDisabled
	_, err = auth.Authenticate(context.Background(), token)
	if !errors.Is(err, ErrUserDisabled) {
		t.Fatalf("Authenticate() error = %v, want ErrUserDisabled", err)
	}
}

func TestJWTRejectsTamperedAndExpiredTokens(t *testing.T) {
	manager := NewJWTManager("test-secret-with-enough-entropy", "bookloop-test", time.Minute)
	token, err := manager.Issue(42)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	userID, err := manager.Verify(token)
	if err != nil || userID != 42 {
		t.Fatalf("Verify() = %d, %v, want 42, nil", userID, err)
	}
	claims := new(jwt.RegisteredClaims)
	parsedToken, _, err := jwt.NewParser().ParseUnverified(token, claims)
	if err != nil {
		t.Fatalf("ParseUnverified() error = %v", err)
	}
	if parsedToken.Method.Alg() != "HS256" || claims.Subject != "42" ||
		claims.Issuer != "bookloop-test" || claims.IssuedAt == nil ||
		claims.ExpiresAt == nil || claims.ID == "" {
		t.Fatalf("JWT claims/method = %s/%+v, want HS256 with sub, iss, iat, exp, jti", parsedToken.Method.Alg(), claims)
	}

	tampered := token[:len(token)-1] + "x"
	if _, err := manager.Verify(tampered); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Verify(tampered) error = %v, want ErrUnauthorized", err)
	}

	expiredManager := NewJWTManager("test-secret-with-enough-entropy", "bookloop-test", -time.Minute)
	expired, err := expiredManager.Issue(42)
	if err != nil {
		t.Fatalf("Issue(expired) error = %v", err)
	}
	if _, err := expiredManager.Verify(expired); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Verify(expired) error = %v, want ErrTokenExpired", err)
	}
	replacement := "x"
	if strings.HasSuffix(expired, replacement) {
		replacement = "y"
	}
	tamperedExpired := expired[:len(expired)-1] + replacement
	if _, err := expiredManager.Verify(tamperedExpired); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Verify(tampered expired) error = %v, want ErrUnauthorized", err)
	}

	wrongIssuerManager := NewJWTManager("test-secret-with-enough-entropy", "another-issuer", time.Minute)
	wrongIssuerToken, err := wrongIssuerManager.Issue(42)
	if err != nil {
		t.Fatalf("Issue(wrong issuer) error = %v", err)
	}
	if _, err := manager.Verify(wrongIssuerToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Verify(wrong issuer) error = %v, want ErrUnauthorized", err)
	}
}

func TestJWTRequiresRegisteredClaimsAndHS256(t *testing.T) {
	secret := "test-secret-with-enough-entropy"
	manager := NewJWTManager(secret, "bookloop-test", time.Minute)

	missingClaims := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Issuer:    "bookloop-test",
		Subject:   "42",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	})
	missingClaimsToken, err := missingClaims.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token with missing claims: %v", err)
	}
	if _, err := manager.Verify(missingClaimsToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Verify(missing claims) error = %v, want ErrUnauthorized", err)
	}

	issuedAt := time.Now().UTC()
	wrongAlgorithm := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.RegisteredClaims{
		Issuer:    "bookloop-test",
		Subject:   "42",
		ExpiresAt: jwt.NewNumericDate(issuedAt.Add(time.Minute)),
		IssuedAt:  jwt.NewNumericDate(issuedAt),
		ID:        "jwt-id",
	})
	wrongAlgorithmToken, err := wrongAlgorithm.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token with wrong algorithm: %v", err)
	}
	if _, err := manager.Verify(wrongAlgorithmToken); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Verify(wrong algorithm) error = %v, want ErrUnauthorized", err)
	}
}

func newTestAuth(t *testing.T, users *fakeUserRepository) *Auth {
	t.Helper()
	auth, err := NewAuth(
		users,
		NewJWTManager("test-secret-with-enough-entropy", "bookloop-test", 15*time.Minute),
		bcrypt.MinCost,
		testRegistration(&fakeRegistrationCodes{accept: true}, &fakeRegistrationMailer{}),
	)
	if err != nil {
		t.Fatalf("NewAuth() error = %v", err)
	}
	return auth
}

type fakeUserRepository struct {
	byID      map[uint64]*model.User
	createErr error
	nextID    uint64
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{byID: make(map[uint64]*model.User), nextID: 1}
}

func (r *fakeUserRepository) Create(_ context.Context, user *model.User) error {
	if r.createErr != nil {
		return r.createErr
	}
	user.ID = r.nextID
	r.nextID++
	user.CreatedAt = time.Now().UTC()
	r.byID[user.ID] = user
	return nil
}

func (r *fakeUserRepository) FindByID(_ context.Context, id uint64) (*model.User, error) {
	user, ok := r.byID[id]
	if !ok {
		return nil, repository.ErrUserNotFound
	}
	return user, nil
}

func (r *fakeUserRepository) FindByUsername(_ context.Context, username string) (*model.User, error) {
	for _, user := range r.byID {
		if strings.EqualFold(user.Username, username) {
			return user, nil
		}
	}
	return nil, repository.ErrUserNotFound
}

func (r *fakeUserRepository) FindByEmail(_ context.Context, email string) (*model.User, error) {
	for _, user := range r.byID {
		if strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	return nil, repository.ErrUserNotFound
}

func (r *fakeUserRepository) addUser(
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
		Email:        email,
		PasswordHash: string(hash),
		Role:         model.UserRoleUser,
		Status:       status,
	}
	if err := r.Create(context.Background(), user); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return user
}
