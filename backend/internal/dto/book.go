package dto

import "time"

type CreateBookRequest struct {
	Title       string `json:"title" binding:"required"`
	Description string `json:"description"`
	Price       string `json:"price" binding:"required"`
	Condition   string `json:"condition" binding:"required"`
}

type UpdateBookRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Price       *string `json:"price"`
	Condition   *string `json:"condition"`
}

type ImageOrderRequest struct {
	ImageIDs []uint64 `json:"image_ids" binding:"required"`
}

type BookListQuery struct {
	Keyword   string
	Condition string
	Sort      string
	Page      int
	PageSize  int
}

type BookImageResponse struct {
	ID        uint64    `json:"id"`
	ImageURL  string    `json:"image_url"`
	SortOrder uint16    `json:"sort_order"`
	CreatedAt time.Time `json:"created_at"`
}

type BookListItemResponse struct {
	ID            uint64    `json:"id"`
	SellerID      uint64    `json:"seller_id"`
	Title         string    `json:"title"`
	Price         string    `json:"price"`
	Condition     string    `json:"condition"`
	Status        string    `json:"status"`
	CoverImageURL *string   `json:"cover_image_url,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type BookResponse struct {
	SellerUsername  string              `json:"seller_username,omitempty"`
	WechatQRCodeURL *string             `json:"wechat_qr_code_url"`
	ID              uint64              `json:"id"`
	SellerID        uint64              `json:"seller_id"`
	Title           string              `json:"title"`
	Description     *string             `json:"description"`
	Price           string              `json:"price"`
	Condition       string              `json:"condition"`
	Status          string              `json:"status"`
	Images          []BookImageResponse `json:"images"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

type BookContactResponse struct {
	WechatQRCodeURL string `json:"wechat_qr_code_url"`
}

type PaginationResponse struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type BookListResponse struct {
	Items      []BookListItemResponse `json:"items"`
	Pagination PaginationResponse     `json:"pagination"`
}
