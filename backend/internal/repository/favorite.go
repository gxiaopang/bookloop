package repository

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gxiaopang/bookloop/backend/internal/model"
)

type FavoriteListRecord struct {
	BookID        uint64
	SellerID      uint64
	Title         string
	Price         string
	Condition     model.BookCondition
	Status        model.BookStatus
	IsAvailable   bool
	CoverImageURL *string
	BookCreatedAt time.Time
	BookUpdatedAt time.Time
	FavoritedAt   time.Time
}

type FavoriteRepository struct {
	db *gorm.DB
}

func NewFavoriteRepository(db *gorm.DB) *FavoriteRepository {
	return &FavoriteRepository{db: db}
}

func (r *FavoriteRepository) Add(ctx context.Context, favorite *model.Favorite) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoUpdates: clause.Assignments(map[string]any{
			"user_id": gorm.Expr("user_id"),
		})}).
		Create(favorite).Error
}

func (r *FavoriteRepository) Remove(ctx context.Context, userID, bookID uint64) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND book_id = ?", userID, bookID).
		Delete(&model.Favorite{}).Error
}

func (r *FavoriteRepository) ListByUser(ctx context.Context, userID uint64, page, pageSize int) ([]FavoriteListRecord, int64, error) {
	database := r.db.WithContext(ctx).
		Table("favorites AS f").
		Where("f.user_id = ?", userID)

	var total int64
	if err := database.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var records []FavoriteListRecord
	err := database.
		Select(`f.book_id, b.seller_id, b.title, b.price, b.condition, b.status,
            CASE WHEN b.status = ? AND u.status = ? THEN TRUE ELSE FALSE END AS is_available,
            (SELECT bi.image_url FROM book_images AS bi
             WHERE bi.book_id = b.id ORDER BY bi.sort_order ASC, bi.id ASC LIMIT 1) AS cover_image_url,
            b.created_at AS book_created_at, b.updated_at AS book_updated_at,
            f.created_at AS favorited_at`, model.BookStatusAvailable, model.UserStatusActive).
		Joins("JOIN books AS b ON b.id = f.book_id").
		Joins("JOIN users AS u ON u.id = b.seller_id").
		Order("f.created_at DESC, f.book_id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Scan(&records).Error
	if err != nil {
		return nil, 0, err
	}
	return records, total, nil
}
