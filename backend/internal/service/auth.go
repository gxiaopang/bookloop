package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/gxiaopang/bookloop/backend/internal/dto"
	"github.com/gxiaopang/bookloop/backend/internal/model"
	"github.com/gxiaopang/bookloop/backend/internal/repository"
)

const (
	minUsernameLength = 3
	maxUsernameLength = 32
	minPasswordLength = 8
	maxPasswordLength = 72
	maxEmailLength    = 254
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type userRepository interface {
	Create(context.Context, *model.User) error
	FindByID(context.Context, uint64) (*model.User, error)
	FindByUsername(context.Context, string) (*model.User, error)
	FindByEmail(context.Context, string) (*model.User, error)
}

type Auth struct {
	users             userRepository
	tokens            *JWTManager
	bcryptCost        int
	dummyPasswordHash []byte
}

type LoginResult struct {
	User        *model.User
	AccessToken string
	ExpiresIn   int64
}

func NewAuth(users userRepository, tokens *JWTManager, bcryptCost int) (*Auth, error) {
	dummyPasswordHash, err := bcrypt.GenerateFromPassword([]byte("bookloop-invalid-login"), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("create dummy password hash: %w", err)
	}
	return &Auth{
		users:             users,
		tokens:            tokens,
		bcryptCost:        bcryptCost,
		dummyPasswordHash: dummyPasswordHash,
	}, nil
}

func (s *Auth) Register(ctx context.Context, request dto.RegisterRequest) (*model.User, error) {
	username := strings.TrimSpace(request.Username)
	email := strings.ToLower(strings.TrimSpace(request.Email))
	if !validUsername(username) || !validEmail(email) || !validPassword(request.Password) {
		return nil, ErrValidation
	}

	if _, err := s.users.FindByUsername(ctx, username); err == nil {
		return nil, ErrUsernameAlreadyExists
	} else if !errors.Is(err, repository.ErrUserNotFound) {
		return nil, err
	}
	if _, err := s.users.FindByEmail(ctx, email); err == nil {
		return nil, ErrEmailAlreadyExists
	} else if !errors.Is(err, repository.ErrUserNotFound) {
		return nil, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.Password), s.bcryptCost)
	if err != nil {
		return nil, err
	}
	user := &model.User{
		Username:     username,
		Email:        email,
		PasswordHash: string(passwordHash),
		Role:         model.UserRoleUser,
		Status:       model.UserStatusActive,
	}
	if err := s.users.Create(ctx, user); err != nil {
		switch {
		case errors.Is(err, repository.ErrUsernameAlreadyExists):
			return nil, ErrUsernameAlreadyExists
		case errors.Is(err, repository.ErrEmailAlreadyExists):
			return nil, ErrEmailAlreadyExists
		default:
			return nil, err
		}
	}
	return user, nil
}

func (s *Auth) Login(ctx context.Context, request dto.LoginRequest) (*LoginResult, error) {
	identifier := strings.TrimSpace(request.Identifier)
	if identifier == "" || request.Password == "" {
		return nil, ErrValidation
	}

	var (
		user *model.User
		err  error
	)
	if strings.Contains(identifier, "@") {
		user, err = s.users.FindByEmail(ctx, strings.ToLower(identifier))
	} else {
		user, err = s.users.FindByUsername(ctx, identifier)
	}
	if errors.Is(err, repository.ErrUserNotFound) {
		// Keep unknown-account failures close to wrong-password failures so the
		// response time does not become a simple account-enumeration signal.
		_ = bcrypt.CompareHashAndPassword(s.dummyPasswordHash, []byte(request.Password))
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.Password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	if user.Status != model.UserStatusActive {
		return nil, ErrUserDisabled
	}

	accessToken, err := s.tokens.Issue(user.ID)
	if err != nil {
		return nil, err
	}
	return &LoginResult{
		User:        user,
		AccessToken: accessToken,
		ExpiresIn:   int64(s.tokens.ExpiresIn() / time.Second),
	}, nil
}

func (s *Auth) Authenticate(ctx context.Context, rawToken string) (*model.User, error) {
	userID, err := s.tokens.Verify(rawToken)
	if err != nil {
		return nil, err
	}
	user, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, repository.ErrUserNotFound) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	if user.Status != model.UserStatusActive {
		return nil, ErrUserDisabled
	}
	return user, nil
}

func validUsername(username string) bool {
	return len(username) >= minUsernameLength && len(username) <= maxUsernameLength && usernamePattern.MatchString(username)
}

func validEmail(email string) bool {
	if email == "" || len(email) > maxEmailLength {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}

func validPassword(password string) bool {
	length := len([]byte(password))
	return length >= minPasswordLength && length <= maxPasswordLength
}
