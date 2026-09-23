package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gxiaopang/bookloop/backend/internal/model"
)

var ErrNotificationNotFound = errors.New("notification not found")

type NotificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

func (r *NotificationRepository) ProcessEvent(
	ctx context.Context,
	consumerName string,
	eventID uint64,
	notifications []model.Notification,
) (bool, error) {
	processed := false
	err := r.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		result := transaction.Clauses(clause.OnConflict{DoNothing: true}).Create(&model.ProcessedMessage{
			ConsumerName: consumerName,
			EventID:      eventID,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		if len(notifications) > 0 {
			if err := transaction.Create(&notifications).Error; err != nil {
				return err
			}
		}
		processed = true
		return nil
	})
	return processed, err
}

func (r *NotificationRepository) ListByUser(
	ctx context.Context,
	userID uint64,
	unreadOnly bool,
	page, pageSize int,
) ([]model.Notification, int64, int64, error) {
	base := r.db.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ?", userID)
	var unread int64
	if err := base.Where("read_at IS NULL").Count(&unread).Error; err != nil {
		return nil, 0, 0, err
	}
	filtered := r.db.WithContext(ctx).Model(&model.Notification{}).Where("user_id = ?", userID)
	if unreadOnly {
		filtered = filtered.Where("read_at IS NULL")
	}
	var total int64
	if err := filtered.Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	var notifications []model.Notification
	if err := filtered.Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&notifications).Error; err != nil {
		return nil, 0, 0, err
	}
	return notifications, total, unread, nil
}

func (r *NotificationRepository) MarkRead(ctx context.Context, userID, notificationID uint64) error {
	var notification model.Notification
	if err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", notificationID, userID).Take(&notification).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotificationNotFound
		}
		return err
	}
	if notification.ReadAt != nil {
		return nil
	}
	return r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("id = ? AND user_id = ? AND read_at IS NULL", notificationID, userID).
		Update("read_at", time.Now().UTC()).Error
}

func (r *NotificationRepository) MarkAllRead(ctx context.Context, userID uint64) error {
	return r.db.WithContext(ctx).Model(&model.Notification{}).
		Where("user_id = ? AND read_at IS NULL", userID).
		Update("read_at", time.Now().UTC()).Error
}
