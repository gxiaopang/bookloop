export type UserRole = 'user' | 'admin'
export type UserStatus = 'active' | 'disabled'
export type BookCondition = 'new' | 'like_new' | 'good' | 'fair'
export type BookStatus = 'available' | 'reserved' | 'sold' | 'off_shelf' | 'blocked'
export type OrderStatus = 'pending_handover' | 'completed' | 'cancelled' | 'expired'

export interface ApiEnvelope<T> {
  code: string
  message: string
  data: T
  request_id: string
}

export interface ApiFailure {
  code: string
  message: string
  details?: unknown
  data?: unknown
  request_id?: string
}

export interface User {
  id: number
  username: string
  email: string
  role: UserRole
  status: UserStatus
  created_at: string
}

export interface LoginResult {
  access_token: string
  token_type: string
  expires_in: number
  user: User
}

export interface Pagination {
  page: number
  page_size: number
  total: number
  total_pages: number
}

export interface Paginated<T> {
  items: T[]
  pagination: Pagination
}

export interface BookImage {
  id: number
  image_url: string
  sort_order: number
  created_at: string
}

export interface BookListItem {
  id: number
  seller_id: number
  title: string
  price: string
  condition: BookCondition
  status: BookStatus
  cover_image_url?: string
  created_at: string
  updated_at: string
}

export interface Book extends Omit<BookListItem, 'cover_image_url'> {
  description?: string
  images: BookImage[]
  wechat_qr_code_url?: string | null
}

export interface BookPayload {
  title: string
  description: string
  price: string
  condition: BookCondition
}

export interface FavoriteItem {
  book_id: number
  seller_id: number
  title: string
  price: string
  condition: BookCondition
  status: BookStatus
  is_available: boolean
  cover_image_url?: string
  book_created_at: string
  book_updated_at: string
  favorited_at: string
}

export interface Order {
  order_no: string
  book_id: number
  buyer_id: number
  seller_id: number
  book_title: string
  book_price: string
  status: OrderStatus
  expires_at: string
  completed_at?: string
  cancelled_at?: string
  cancel_reason?: string
  created_at: string
  updated_at: string
}

export interface BookListParams {
  keyword?: string
  condition?: BookCondition | ''
  sort?: 'newest' | 'price_asc' | 'price_desc'
  page?: number
  page_size?: number
}

