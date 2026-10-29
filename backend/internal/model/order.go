package model

import "time"

type OrderStatus string

const (
	OrderStatusPendingHandover OrderStatus = "pending_handover"
	OrderStatusCompleted       OrderStatus = "completed"
	OrderStatusCancelled       OrderStatus = "cancelled"
	OrderStatusExpired         OrderStatus = "expired"
)

type Order struct {
	ID                 uint64      `gorm:"column:id;primaryKey;autoIncrement"`
	OrderNo            string      `gorm:"column:order_no;size:26;not null"`
	BookID             uint64      `gorm:"column:book_id;not null"`
	BuyerID            uint64      `gorm:"column:buyer_id;not null"`
	SellerID           uint64      `gorm:"column:seller_id;not null"`
	BookTitle          string      `gorm:"column:book_title;size:200;not null"`
	BookPrice          string      `gorm:"column:book_price;type:decimal(10,2);not null"`
	Status             OrderStatus `gorm:"column:status;size:24;not null"`
	IdempotencyKey     string      `gorm:"column:idempotency_key;size:64;not null"`
	RequestFingerprint string      `gorm:"column:request_fingerprint;size:64;not null"`
	ExpiresAt          time.Time   `gorm:"column:expires_at;not null"`
	CompletedAt        *time.Time  `gorm:"column:completed_at"`
	CancelledAt        *time.Time  `gorm:"column:cancelled_at"`
	CancelReason       *string     `gorm:"column:cancel_reason;size:255"`
	ActiveBookID       *uint64     `gorm:"column:active_book_id;->"`
	CreatedAt          time.Time   `gorm:"column:created_at;not null"`
	UpdatedAt          time.Time   `gorm:"column:updated_at;not null"`
}

func (Order) TableName() string {
	return "orders"
}
