package model

import "time"

type BookCondition string

const (
	BookConditionNew     BookCondition = "new"
	BookConditionLikeNew BookCondition = "like_new"
	BookConditionGood    BookCondition = "good"
	BookConditionFair    BookCondition = "fair"
)

type BookStatus string

const (
	BookStatusAvailable BookStatus = "available"
	BookStatusReserved  BookStatus = "reserved"
	BookStatusSold      BookStatus = "sold"
	BookStatusOffShelf  BookStatus = "off_shelf"
	BookStatusBlocked   BookStatus = "blocked"
)

type Book struct {
	ID          uint64        `gorm:"column:id;primaryKey;autoIncrement"`
	SellerID    uint64        `gorm:"column:seller_id;not null"`
	Title       string        `gorm:"column:title;size:200;not null"`
	Description *string       `gorm:"column:description;type:text"`
	Price       string        `gorm:"column:price;type:decimal(10,2);not null"`
	Condition   BookCondition `gorm:"column:condition;size:16;not null"`
	Status      BookStatus    `gorm:"column:status;size:20;not null;default:off_shelf"`
	CreatedAt   time.Time     `gorm:"column:created_at;not null"`
	UpdatedAt   time.Time     `gorm:"column:updated_at;not null"`
}

func (Book) TableName() string {
	return "books"
}

type BookImage struct {
	ID        uint64    `gorm:"column:id;primaryKey;autoIncrement"`
	BookID    uint64    `gorm:"column:book_id;not null"`
	ImageURL  string    `gorm:"column:image_url;size:2048;not null"`
	SortOrder uint16    `gorm:"column:sort_order;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (BookImage) TableName() string {
	return "book_images"
}
