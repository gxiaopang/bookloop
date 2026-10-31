<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { errorMessage } from '@/api/http'
import type { Order, Pagination } from '@/types/api'
import { formatTime, orderStatusLabels, statusType } from '@/utils/presentation'
import PageFeedback from '@/components/PageFeedback.vue'
import PageHeader from '@/components/PageHeader.vue'

const role = ref<'buyer' | 'seller'>('buyer')
const orders = ref<Order[]>([])
const pagination = ref<Pagination>({ page: 1, page_size: 10, total: 0, total_pages: 0 })
const loading = ref(false)
const error = ref('')
const acting = ref('')
const cancelReasonLabels: Record<string, string> = {
  'cancelled by buyer': '买家已取消订单',
  'cancelled by seller': '卖家已取消订单',
  'reservation timeout': '预约超时，订单已关闭',
}

async function load(page = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await api.listOrders(role.value, { page, page_size: 10 })
    orders.value = result.items
    pagination.value = result.pagination
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

async function action(order: Order, operation: 'cancel' | 'deliver' | 'complete') {
  if (acting.value) return
  const labels = {
    cancel: '确认取消这笔订单？',
    deliver: '确认已经将图书交给买家？确认后将等待买家收书，订单不再自动过期或允许取消。',
    complete: '确认已实际收到图书？确认后交易完成。',
  }
  try {
    await ElMessageBox.confirm(labels[operation], '订单操作', { type: operation === 'cancel' ? 'warning' : 'info' })
  } catch {
    return
  }
  acting.value = `${order.order_no}:${operation}`
  try {
    const updated = operation === 'cancel'
      ? await api.cancelOrder(order.order_no)
      : operation === 'deliver'
        ? await api.deliverOrder(order.order_no)
        : await api.completeOrder(order.order_no)
    const index = orders.value.findIndex((item) => item.order_no === updated.order_no)
    if (index >= 0) orders.value[index] = updated
    ElMessage.success('订单状态已更新')
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    acting.value = ''
  }
}

function changeRole(next: string | number) {
  role.value = next as 'buyer' | 'seller'
  void load()
}

onMounted(() => load())
</script>

<template>
  <section class="page-width inner-page">
    <PageHeader eyebrow="TRANSACTIONS" title="我的订单" description="微信协商 → 卖家确认交付 → 买家确认收书 → 交易完成。" />
    <el-segmented class="order-role-switch" :model-value="role" :options="[{ label: '我买到的', value: 'buyer' }, { label: '我卖出的', value: 'seller' }]" @change="changeRole" />
    <PageFeedback :loading="loading" :error="error" :empty="!orders.length" empty-title="暂时没有订单" :empty-text="role === 'buyer' ? '从书市挑一本喜欢的书开始吧。' : '发布图书后，订单会显示在这里。'" @retry="load(pagination.page)" />
    <div v-if="!loading && !error && orders.length" class="order-list">
      <article v-for="order in orders" :key="order.order_no" class="order-card">
        <header>
          <div><span>订单号</span><code>{{ order.order_no }}</code></div>
          <el-tag :type="statusType(order.status)" effect="light">{{ orderStatusLabels[order.status] }}</el-tag>
        </header>
        <div class="order-main">
          <div>
            <h3>{{ order.book_title }}</h3>
            <p>{{ role === 'buyer' ? '卖家' : '买家' }}：{{ (role === 'buyer' ? order.seller_username : order.buyer_username) || '暂无用户名' }} · 创建于 {{ formatTime(order.created_at) }}</p>
          </div>
          <strong>¥{{ order.book_price }}</strong>
        </div>
        <footer>
          <span v-if="order.status === 'pending_handover'">预约截止：{{ formatTime(order.expires_at) }}</span>
          <span v-else-if="order.status === 'pending_receipt'">卖家已于 {{ formatTime(order.handed_over_at || undefined) }} 确认交付，等待买家确认收书</span>
          <span v-else-if="order.completed_at">完成时间：{{ formatTime(order.completed_at) }}</span>
          <span v-else>{{ cancelReasonLabels[order.cancel_reason || ''] || order.cancel_reason || '订单已结束' }}</span>
          <div class="order-actions">
            <el-button v-if="order.status === 'pending_handover'" :disabled="Boolean(acting)" :loading="acting === `${order.order_no}:cancel`" @click="action(order, 'cancel')">取消订单</el-button>
            <el-button v-if="role === 'seller' && order.status === 'pending_handover'" type="primary" :disabled="Boolean(acting)" :loading="acting === `${order.order_no}:deliver`" @click="action(order, 'deliver')">确认交付</el-button>
            <el-button v-if="role === 'buyer' && order.status === 'pending_receipt'" type="primary" :disabled="Boolean(acting)" :loading="acting === `${order.order_no}:complete`" @click="action(order, 'complete')">确认收书</el-button>
          </div>
        </footer>
      </article>
    </div>
    <el-pagination v-if="pagination.total_pages > 1" class="pagination" layout="prev, pager, next" :current-page="pagination.page" :page-size="pagination.page_size" :total="pagination.total" @current-change="load" />
  </section>
</template>
