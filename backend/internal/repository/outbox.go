package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/gxiaopang/bookloop/backend/internal/model"
)

var ErrOutboxLeaseLost = errors.New("outbox lease lost")

type OutboxStats struct {
	Pending       int64
	Retrying      int64
	OldestPending *time.Time
}

type OutboxRepository struct {
	db *gorm.DB
}

func NewOutboxRepository(db *gorm.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

func (r *OutboxRepository) ClaimBatch(
	ctx context.Context,
	workerID string,
	limit int,
	leaseDuration time.Duration,
) ([]model.OutboxEvent, error) {
	var events []model.OutboxEvent
	cutoff := time.Now().UTC().Add(-leaseDuration)
	err := r.db.WithContext(ctx).Transaction(func(transaction *gorm.DB) error {
		if err := transaction.
			Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("published_at IS NULL").
			Where("available_at <= UTC_TIMESTAMP(3)").
			Where("next_attempt_at IS NULL OR next_attempt_at <= UTC_TIMESTAMP(3)").
			Where("locked_at IS NULL OR locked_at < ?", cutoff).
			Order("id ASC").
			Limit(limit).
			Find(&events).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		ids := make([]uint64, 0, len(events))
		for index := range events {
			ids = append(ids, events[index].ID)
		}
		now := time.Now().UTC()
		if err := transaction.Model(&model.OutboxEvent{}).
			Where("id IN ?", ids).
			Updates(map[string]any{"locked_by": workerID, "locked_at": now}).Error; err != nil {
			return err
		}
		for index := range events {
			events[index].LockedBy = &workerID
			events[index].LockedAt = &now
		}
		return nil
	})
	return events, err
}

func (r *OutboxRepository) MarkPublished(ctx context.Context, eventID uint64, workerID string) error {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&model.OutboxEvent{}).
		Where("id = ? AND published_at IS NULL AND locked_by = ?", eventID, workerID).
		Updates(map[string]any{
			"published_at":    now,
			"locked_by":       nil,
			"locked_at":       nil,
			"next_attempt_at": nil,
			"last_error":      nil,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrOutboxLeaseLost
	}
	return nil
}

func (r *OutboxRepository) ReleaseWithError(
	ctx context.Context,
	eventID uint64,
	workerID, message string,
	retryAt time.Time,
) error {
	if len(message) > 500 {
		message = message[:500]
	}
	result := r.db.WithContext(ctx).Model(&model.OutboxEvent{}).
		Where("id = ? AND published_at IS NULL AND locked_by = ?", eventID, workerID).
		Updates(map[string]any{
			"attempts":        gorm.Expr("attempts + 1"),
			"next_attempt_at": retryAt.UTC(),
			"locked_by":       nil,
			"locked_at":       nil,
			"last_error":      message,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrOutboxLeaseLost
	}
	return nil
}

func (r *OutboxRepository) Stats(ctx context.Context) (OutboxStats, error) {
	var stats OutboxStats
	base := func() *gorm.DB {
		return r.db.WithContext(ctx).Model(&model.OutboxEvent{}).Where("published_at IS NULL")
	}
	if err := base().Count(&stats.Pending).Error; err != nil {
		return OutboxStats{}, err
	}
	if err := base().Where("attempts > 0").Count(&stats.Retrying).Error; err != nil {
		return OutboxStats{}, err
	}
	var oldest struct{ OccurredAt *time.Time }
	if err := base().Select("MIN(occurred_at) AS occurred_at").Scan(&oldest).Error; err != nil {
		return OutboxStats{}, err
	}
	stats.OldestPending = oldest.OccurredAt
	return stats, nil
}
