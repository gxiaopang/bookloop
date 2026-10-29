package event

import (
	"encoding/json"
	"time"
)

const (
	OrderCreated   = "order.created.v1"
	OrderCancelled = "order.cancelled.v1"
	OrderExpired   = "order.expired.v1"
	OrderCompleted = "order.completed.v1"
)

type Envelope struct {
	ID            uint64          `json:"id"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   string          `json:"aggregate_id"`
	Type          string          `json:"type"`
	OccurredAt    time.Time       `json:"occurred_at"`
	Payload       json.RawMessage `json:"payload"`
}

type OrderPayload struct {
	OrderNo   string    `json:"order_no"`
	BookID    uint64    `json:"book_id"`
	BuyerID   uint64    `json:"buyer_id"`
	SellerID  uint64    `json:"seller_id"`
	BookTitle string    `json:"book_title"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
}
