package model

import (
	"encoding/json"
	"time"
)

type OutboxEvent struct {
	ID            uint64          `gorm:"column:id;primaryKey;autoIncrement"`
	AggregateType string          `gorm:"column:aggregate_type;size:32;not null"`
	AggregateID   string          `gorm:"column:aggregate_id;size:64;not null"`
	EventType     string          `gorm:"column:event_type;size:64;not null"`
	Payload       json.RawMessage `gorm:"column:payload;type:json;not null"`
	OccurredAt    time.Time       `gorm:"column:occurred_at;not null"`
	AvailableAt   time.Time       `gorm:"column:available_at;not null"`
	PublishedAt   *time.Time      `gorm:"column:published_at"`
	Attempts      uint            `gorm:"column:attempts;not null"`
	NextAttemptAt *time.Time      `gorm:"column:next_attempt_at"`
	LockedBy      *string         `gorm:"column:locked_by;size:64"`
	LockedAt      *time.Time      `gorm:"column:locked_at"`
	LastError     *string         `gorm:"column:last_error;size:500"`
	CreatedAt     time.Time       `gorm:"column:created_at;not null"`
}

func (OutboxEvent) TableName() string { return "outbox_events" }
