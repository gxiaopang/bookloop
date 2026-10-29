package dto

import "time"

type CreateOrderRequest struct {
	BookID uint64 `json:"book_id" binding:"required"`
}

type OrderListQuery struct {
	As       string
	Page     int
	PageSize int
}

type OrderResponse struct {
	OrderNo      string     `json:"order_no"`
	BookID       uint64     `json:"book_id"`
	BuyerID      uint64     `json:"buyer_id"`
	SellerID     uint64     `json:"seller_id"`
	BookTitle    string     `json:"book_title"`
	BookPrice    string     `json:"book_price"`
	Status       string     `json:"status"`
	ExpiresAt    time.Time  `json:"expires_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	CancelledAt  *time.Time `json:"cancelled_at"`
	CancelReason *string    `json:"cancel_reason"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type OrderListResponse struct {
	Items      []OrderResponse    `json:"items"`
	Pagination PaginationResponse `json:"pagination"`
}
