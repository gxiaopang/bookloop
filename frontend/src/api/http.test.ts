import { describe, expect, it, vi } from 'vitest'

vi.mock('@/services/authSession', () => ({
  authSession: { getToken: () => null, clear: vi.fn() },
}))

import { ApiError, errorMessage } from './http'

describe('user-facing API errors', () => {
  it('keeps request IDs available for diagnostics without showing them in the message', () => {
    const error = new ApiError('authentication required', 'UNAUTHORIZED', 401, 'web-internal-trace-123')
    expect(errorMessage(error)).toBe('请先登录')
    expect(error.requestId).toBe('web-internal-trace-123')
  })
})
