import type { BookCondition, BookStatus, OrderStatus } from '@/types/api'

export const conditionLabels: Record<BookCondition, string> = {
  new: '全新',
  like_new: '近全新',
  good: '品相良好',
  fair: '有使用痕迹',
}

export const bookStatusLabels: Record<BookStatus, string> = {
  available: '在售',
  reserved: '已锁定',
  sold: '已售出',
  off_shelf: '已下架',
  blocked: '已屏蔽',
}

export const orderStatusLabels: Record<OrderStatus, string> = {
  pending_handover: '待线下交付',
  completed: '已完成',
  cancelled: '已取消',
  expired: '已过期',
}

export function formatTime(value?: string): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  }).format(new Date(value))
}

export function statusType(status: string): 'success' | 'warning' | 'info' | 'danger' | 'primary' {
  if (status === 'available' || status === 'completed') return 'success'
  if (status === 'pending_handover' || status === 'reserved') return 'warning'
  if (status === 'blocked' || status === 'expired') return 'danger'
  return 'info'
}

