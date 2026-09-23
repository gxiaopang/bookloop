package dto

import "time"

type NotificationResponse struct {
	ID        uint64     `json:"id"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Content   string     `json:"content"`
	OrderNo   *string    `json:"order_no,omitempty"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

type NotificationListResponse struct {
	Items       []NotificationResponse `json:"items"`
	UnreadCount int64                  `json:"unread_count"`
	Pagination  PaginationResponse     `json:"pagination"`
}

type MessagingStatusResponse struct {
	PendingOutbox       int64      `json:"pending_outbox"`
	RetryingOutbox      int64      `json:"retrying_outbox"`
	OldestPendingAt     *time.Time `json:"oldest_pending_at,omitempty"`
	OldestPendingAgeSec int64      `json:"oldest_pending_age_seconds"`
}
