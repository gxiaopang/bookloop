package model

import "time"

type Favorite struct {
	UserID    uint64    `gorm:"column:user_id;primaryKey;autoIncrement:false"`
	BookID    uint64    `gorm:"column:book_id;primaryKey;autoIncrement:false"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (Favorite) TableName() string {
	return "favorites"
}
