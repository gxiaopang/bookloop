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

async function action(order: Order, operation: 'cancel' | 'complete') {
  const labels = { cancel: '确认取消这笔订单？', complete: '确认已经完成线下交付？' }
  try {
    await ElMessageBox.confirm(labels[operation], '订单操作', { type: operation === 'cancel' ? 'warning' : 'info' })
  } catch {
    return
  }
  acting.value = `${order.order_no}:${operation}`
  try {
    const updated = operation === 'cancel'
      ? await api.cancelOrder(order.order_no)
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
    <PageHeader eyebrow="TRANSACTIONS" title="我的订单" description="通过卖家微信协商线下交易，交付后由买家确认完成。" />
    <el-segmented :model-value="role" :options="[{ label: '我买到的', value: 'buyer' }, { label: '我卖出的', value: 'seller' }]" @change="changeRole" />
    <PageFeedback :loading="loading" :error="error" :empty="!orders.length" empty-title="暂时没有订单" :empty-text="role === 'buyer' ? '从书市挑一本喜欢的书开始吧。' : '发布图书后，订单会显示在这里。'" @retry="load(pagination.page)" />
    <div v-if="!loading && !error && orders.length" class="order-list">
      <article v-for="order in orders" :key="order.order_no" class="order-card">
        <header>
          <div><span>订单号</span><code>{{ order.order_no }}</code></div>
          <el-tag :type="statusType(order.status)" effect="light">{{ orderStatusLabels[order.status] }}</el-tag>
        </header>
        <div class="order-main">
          <div>
            <span class="eyebrow">图书 #{{ order.book_id }}</span>
            <h3>{{ order.book_title }}</h3>
            <p>{{ role === 'buyer' ? `卖家 #${order.seller_id}` : `买家 #${order.buyer_id}` }} · 创建于 {{ formatTime(order.created_at) }}</p>
          </div>
          <strong>¥{{ order.book_price }}</strong>
        </div>
        <footer>
          <span v-if="order.status === 'pending_handover'">预约截止：{{ formatTime(order.expires_at) }}</span>
          <span v-else-if="order.completed_at">完成时间：{{ formatTime(order.completed_at) }}</span>
          <span v-else>{{ order.cancel_reason || '订单已结束' }}</span>
          <div v-if="role === 'buyer'" class="order-actions">
            <template v-if="order.status === 'pending_handover'">
              <el-button :loading="acting === `${order.order_no}:cancel`" @click="action(order, 'cancel')">取消订单</el-button>
              <el-button type="primary" :loading="acting === `${order.order_no}:complete`" @click="action(order, 'complete')">确认线下交付</el-button>
            </template>
          </div>
        </footer>
      </article>
    </div>
    <el-pagination v-if="pagination.total_pages > 1" class="pagination" layout="prev, pager, next" :current-page="pagination.page" :page-size="pagination.page_size" :total="pagination.total" @current-change="load" />
  </section>
</template>
