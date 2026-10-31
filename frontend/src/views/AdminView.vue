<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { errorMessage } from '@/api/http'
import type { Order, Pagination } from '@/types/api'
import { formatTime, orderStatusLabels, statusType } from '@/utils/presentation'
import PageFeedback from '@/components/PageFeedback.vue'
import PageHeader from '@/components/PageHeader.vue'

const orders = ref<Order[]>([])
const pagination = ref<Pagination>({ page: 1, page_size: 20, total: 0, total_pages: 0 })
const loading = ref(false)
const error = ref('')
const bookId = ref<number | undefined>()
const moderating = ref(false)

async function load(page = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await api.listAllOrders({ page, page_size: 20 })
    orders.value = result.items
    pagination.value = result.pagination
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

async function moderate(action: 'block' | 'unblock') {
  if (!bookId.value || bookId.value < 1) {
    ElMessage.warning('请输入有效的图书编号')
    return
  }
  const text = action === 'block' ? '屏蔽' : '解除屏蔽'
  try {
    await ElMessageBox.confirm(`确认${text}编号为 ${bookId.value} 的图书？`, '图书治理', { type: 'warning' })
  } catch {
    return
  }
  moderating.value = true
  try {
    const result = action === 'block' ? await api.blockBook(bookId.value) : await api.unblockBook(bookId.value)
    ElMessage.success(`《${result.title}》已${text}`)
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    moderating.value = false
  }
}

onMounted(() => load())
</script>

<template>
  <section class="page-width inner-page admin-page">
    <PageHeader eyebrow="ADMIN CONSOLE" title="基础管理台" description="查看交易记录，处理违规图书。" />
    <section class="moderation-card">
      <div><span class="eyebrow">BOOK MODERATION</span><h2>图书治理</h2><p>输入图书编号定位图书，进行屏蔽或解除屏蔽。</p></div>
      <div class="moderation-actions">
        <el-input-number v-model="bookId" :min="1" :precision="0" placeholder="图书编号" aria-label="图书编号" />
        <el-button type="danger" :loading="moderating" @click="moderate('block')">屏蔽图书</el-button>
        <el-button :loading="moderating" @click="moderate('unblock')">解除屏蔽</el-button>
      </div>
    </section>
    <section class="audit-section">
      <div class="section-intro"><div><span class="eyebrow">ORDER AUDIT</span><h2>订单审计</h2></div><span class="result-count">{{ pagination.total }} 笔</span></div>
      <PageFeedback :loading="loading" :error="error" :empty="!orders.length" empty-title="暂无订单记录" @retry="load(pagination.page)" />
      <el-table v-if="!loading && !error && orders.length" :data="orders" stripe>
        <el-table-column prop="order_no" label="订单号" min-width="230" />
        <el-table-column prop="book_title" label="图书" min-width="180" />
        <el-table-column label="买家" min-width="140"><template #default="scope">{{ scope.row.buyer_username || '暂无用户名' }}</template></el-table-column>
        <el-table-column label="卖家" min-width="140"><template #default="scope">{{ scope.row.seller_username || '暂无用户名' }}</template></el-table-column>
        <el-table-column label="金额" width="100"><template #default="scope">¥{{ scope.row.book_price }}</template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="scope"><el-tag :type="statusType(scope.row.status)">{{ orderStatusLabels[scope.row.status as Order['status']] }}</el-tag></template></el-table-column>
        <el-table-column label="创建时间" min-width="170"><template #default="scope">{{ formatTime(scope.row.created_at) }}</template></el-table-column>
      </el-table>
      <el-pagination v-if="pagination.total_pages > 1" class="pagination" layout="prev, pager, next" :current-page="pagination.page" :page-size="pagination.page_size" :total="pagination.total" @current-change="load" />
    </section>
  </section>
</template>
