import axios, { AxiosError, AxiosHeaders } from 'axios'
import type { ApiFailure } from '@/types/api'
import { authSession } from '@/services/authSession'

export class ApiError extends Error {
  constructor(
    message: string,
    readonly code: string,
    readonly status: number | undefined,
    readonly requestId?: string,
    readonly details?: unknown,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export const http = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api/v1',
  timeout: 12_000,
})

http.interceptors.request.use((config) => {
  const headers = AxiosHeaders.from(config.headers)
  const token = authSession.getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)
  if (!headers.has('X-Request-ID')) headers.set('X-Request-ID', createRequestId())
  // Idempotency-Key is intentionally absent here. Order creation owns its key.
  config.headers = headers
  return config
})

http.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ApiFailure>) => {
    const failure = error.response?.data
    const normalized = new ApiError(
      failure?.message || (error.response ? '请求失败，请稍后重试' : '网络连接失败，请检查后重试'),
      failure?.code || (error.response ? 'HTTP_ERROR' : 'NETWORK_ERROR'),
      error.response?.status,
      failure?.request_id || error.response?.headers['x-request-id'],
      failure?.details,
    )
    if (error.response?.status === 401 && authSession.getToken()) {
      authSession.clear()
      window.dispatchEvent(new CustomEvent('bookloop:unauthorized'))
    }
    return Promise.reject(normalized)
  },
)

function createRequestId(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return `web-${crypto.randomUUID()}`
  }
  return `web-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const localized = errorMessages[error.code]
    return `${localized || error.message}${error.requestId ? `（请求 ID：${error.requestId}）` : ''}`
  }
  return error instanceof Error ? error.message : '发生未知错误'
}

const errorMessages: Record<string, string> = {
  VALIDATION_ERROR: '提交内容不符合要求，请检查后重试',
  INVALID_CREDENTIALS: '用户名、邮箱或密码不正确',
  USERNAME_ALREADY_EXISTS: '该用户名已被使用',
  EMAIL_ALREADY_EXISTS: '该邮箱已被使用',
  UNAUTHORIZED: '请先登录',
  TOKEN_EXPIRED: '登录已过期，请重新登录',
  USER_DISABLED: '账号已被停用',
  FORBIDDEN: '没有执行此操作的权限',
  BOOK_NOT_FOUND: '图书不存在或当前不可见',
  BOOK_NOT_OWNER: '只能管理自己发布的图书',
  BOOK_STATE_CONFLICT: '图书当前状态不允许此操作',
  BOOK_CONTACT_REQUIRED: '请先上传微信联系二维码，再上架商品',
  BOOK_IMAGE_REQUIRED: '上架前至少需要一张实物图片',
  BOOK_IMAGE_LIMIT_REACHED: '每本图书最多上传六张图片',
  INVALID_IMAGE: '仅支持有效的 JPEG 或 PNG 图片',
  IMAGE_TOO_LARGE: '图片超过服务端允许的大小',
  BOOK_NOT_AVAILABLE: '这本书已不可购买',
  CANNOT_PURCHASE_OWN_BOOK: '不能购买自己发布的图书',
  ORDER_STATE_CONFLICT: '订单当前状态不允许此操作',
  REQUEST_IN_PROGRESS: '购买请求仍在处理中，请使用同一意图重试',
  SERVICE_UNAVAILABLE: '服务暂时不可用，请稍后重试',
}

