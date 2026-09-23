package model

import "time"

type Notification struct {
	ID        uint64     `gorm:"column:id;primaryKey;autoIncrement"`
	UserID    uint64     `gorm:"column:user_id;not null"`
	EventID   uint64     `gorm:"column:event_id;not null"`
	Type      string     `gorm:"column:type;size:64;not null"`
	Title     string     `gorm:"column:title;size:100;not null"`
	Content   string     `gorm:"column:content;size:500;not null"`
	OrderNo   *string    `gorm:"column:order_no;size:26"`
	ReadAt    *time.Time `gorm:"column:read_at"`
	CreatedAt time.Time  `gorm:"column:created_at;not null"`
}

func (Notification) TableName() string { return "notifications" }

type ProcessedMessage struct {
	ConsumerName string    `gorm:"column:consumer_name;size:64;primaryKey"`
	EventID      uint64    `gorm:"column:event_id;primaryKey"`
	ProcessedAt  time.Time `gorm:"column:processed_at;not null;autoCreateTime"`
}

func (ProcessedMessage) TableName() string { return "processed_messages" }
