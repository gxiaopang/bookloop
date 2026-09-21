package repository

import (
	"context"
	"errors"
	"strings"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"github.com/gxiaopang/bookloop/backend/internal/model"
)

var (
	ErrUserNotFound          = errors.New("user not found")
	ErrUsernameAlreadyExists = errors.New("username already exists")
	ErrEmailAlreadyExists    = errors.New("email already exists")
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	err := r.db.WithContext(ctx).Create(user).Error
	if err == nil {
		return nil
	}

	var mysqlError *drivermysql.MySQLError
	if errors.As(err, &mysqlError) && mysqlError.Number == 1062 {
		switch {
		case strings.Contains(mysqlError.Message, "ux_users_username"):
			return ErrUsernameAlreadyExists
		case strings.Contains(mysqlError.Message, "ux_users_email"):
			return ErrEmailAlreadyExists
		}
	}
	return err
}

func (r *UserRepository) FindByID(ctx context.Context, id uint64) (*model.User, error) {
	return r.findOne(ctx, "id = ?", id)
}

func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	return r.findOne(ctx, "username = ?", username)
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*model.User, error) {
	return r.findOne(ctx, "email = ?", email)
}

func (r *UserRepository) findOne(ctx context.Context, query string, value any) (*model.User, error) {
	var user model.User
	if err := r.db.WithContext(ctx).Where(query, value).Take(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return &user, nil
}
