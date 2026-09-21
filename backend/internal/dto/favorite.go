package dto

import "time"

type FavoriteListItemResponse struct {
	BookID        uint64    `json:"book_id"`
	SellerID      uint64    `json:"seller_id"`
	Title         string    `json:"title"`
	Price         string    `json:"price"`
	Condition     string    `json:"condition"`
	Status        string    `json:"status"`
	IsAvailable   bool      `json:"is_available"`
	CoverImageURL *string   `json:"cover_image_url,omitempty"`
	BookCreatedAt time.Time `json:"book_created_at"`
	BookUpdatedAt time.Time `json:"book_updated_at"`
	FavoritedAt   time.Time `json:"favorited_at"`
}

type FavoriteListResponse struct {
	Items      []FavoriteListItemResponse `json:"items"`
	Pagination PaginationResponse         `json:"pagination"`
}
