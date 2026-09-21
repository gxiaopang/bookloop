import type {
  ApiEnvelope,
  Book,
  BookImage,
  BookListItem,
  BookListParams,
  BookPayload,
  FavoriteItem,
  LoginResult,
  Order,
  Paginated,
  User,
} from '@/types/api'
import { http } from './http'

async function data<T>(request: Promise<{ data: ApiEnvelope<T> }>): Promise<T> {
  return (await request).data.data
}

export const api = {
  register(payload: { username: string; email: string; password: string }) {
    return data<User>(http.post('/auth/register', payload))
  },
  login(payload: { identifier: string; password: string }) {
    return data<LoginResult>(http.post('/auth/login', payload))
  },
  currentUser() {
    return data<User>(http.get('/users/me'))
  },
  listBooks(params: BookListParams) {
    return data<Paginated<BookListItem>>(http.get('/books', { params }))
  },
  getBook(bookId: number) {
    return data<Book>(http.get(`/books/${bookId}`))
  },
  listMyBooks(params: { page: number; page_size: number }) {
    return data<Paginated<BookListItem>>(http.get('/users/me/books', { params }))
  },
  getMyBook(bookId: number) {
    return data<Book>(http.get(`/users/me/books/${bookId}`))
  },
  createBook(payload: BookPayload) {
    return data<Book>(http.post('/books', payload))
  },
  updateBook(bookId: number, payload: Partial<BookPayload>) {
    return data<Book>(http.patch(`/books/${bookId}`, payload))
  },
  uploadBookImage(bookId: number, file: File) {
    const form = new FormData()
    form.append('image', file)
    return data<BookImage>(http.post(`/books/${bookId}/images`, form))
  },
  deleteBookImage(bookId: number, imageId: number) {
    return data<{ deleted: boolean }>(http.delete(`/books/${bookId}/images/${imageId}`))
  },
  reorderBookImages(bookId: number, imageIds: number[]) {
    return data<BookImage[]>(http.put(`/books/${bookId}/images/order`, { image_ids: imageIds }))
  },
  putBookOnShelf(bookId: number) {
    return data<Book>(http.post(`/books/${bookId}/on-shelf`))
  },
  takeBookOffShelf(bookId: number) {
    return data<Book>(http.post(`/books/${bookId}/off-shelf`))
  },
  listFavorites(params: { page: number; page_size: number }) {
    return data<Paginated<FavoriteItem>>(http.get('/favorites', { params }))
  },
  addFavorite(bookId: number) {
    return data<{ favorited: boolean }>(http.put(`/favorites/${bookId}`))
  },
  removeFavorite(bookId: number) {
    return data<{ favorited: boolean }>(http.delete(`/favorites/${bookId}`))
  },
  createOrder(bookId: number, idempotencyKey: string) {
    return data<Order>(http.post('/orders', { book_id: bookId }, { headers: { 'Idempotency-Key': idempotencyKey } }))
  },
  listOrders(as: 'buyer' | 'seller', params: { page: number; page_size: number }) {
    return data<Paginated<Order>>(http.get('/orders', { params: { as, ...params } }))
  },
  payOrder(orderNo: string) {
    return data<Order>(http.post(`/orders/${orderNo}/pay`))
  },
  cancelOrder(orderNo: string) {
    return data<Order>(http.post(`/orders/${orderNo}/cancel`))
  },
  completeOrder(orderNo: string) {
    return data<Order>(http.post(`/orders/${orderNo}/complete`))
  },
  listAllOrders(params: { page: number; page_size: number }) {
    return data<Paginated<Order>>(http.get('/admin/orders', { params }))
  },
  blockBook(bookId: number) {
    return data<Book>(http.post(`/admin/books/${bookId}/block`))
  },
  unblockBook(bookId: number) {
    return data<Book>(http.post(`/admin/books/${bookId}/unblock`))
  },
}

export function assetUrl(path?: string): string {
  if (!path) return ''
  if (/^https?:\/\//.test(path)) return path
  return `${import.meta.env.VITE_ASSET_BASE_URL || ''}${path}`
}

