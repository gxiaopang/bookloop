<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, assetUrl } from '@/api'
import { ApiError, errorMessage } from '@/api/http'
import { purchaseIntents } from '@/services/purchaseIntent'
import { useAuthStore } from '@/stores/auth'
import type { Book, Order } from '@/types/api'
import { conditionLabels } from '@/utils/presentation'
import BookCover from '@/components/BookCover.vue'
import PageFeedback from '@/components/PageFeedback.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const bookId = Number(route.params.id)
const book = ref<Book | null>(null)
const loading = ref(false)
const buying = ref(false)
const favoriteBusy = ref(false)
const contactVisible = ref(false)
const error = ref('')
const purchaseError = ref('')
const order = ref<Order | null>(null)
const pendingKey = ref(purchaseIntents.current(bookId))
const isOwnBook = computed(() => auth.user?.id === book.value?.seller_id)

async function load() {
  if (!Number.isInteger(bookId) || bookId < 1) {
    error.value = '图书链接无效，请从书市重新进入'
    return
  }
  loading.value = true
  error.value = ''
  try {
    book.value = await api.getBook(bookId)
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

function requireLogin(): boolean {
  if (auth.isAuthenticated) return true
  void router.push({ name: 'auth', query: { redirect: route.fullPath } })
  return false
}

async function addFavorite() {
  if (!requireLogin()) return
  favoriteBusy.value = true
  try {
    await api.addFavorite(bookId)
    ElMessage.success('已加入收藏')
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    favoriteBusy.value = false
  }
}

async function buy() {
  if (!requireLogin() || isOwnBook.value) return
  const key = purchaseIntents.begin(bookId)
  pendingKey.value = key
  purchaseError.value = ''
  buying.value = true
  try {
    order.value = await api.createOrder(bookId, key)
    purchaseIntents.complete(bookId, key)
    pendingKey.value = null
    ElMessage.success('订单已创建，图书已为你保留')
  } catch (cause) {
    purchaseError.value = errorMessage(cause)
    if (cause instanceof ApiError && ['BOOK_NOT_AVAILABLE', 'CANNOT_PURCHASE_OWN_BOOK', 'IDEMPOTENCY_KEY_REUSED'].includes(cause.code)) {
      purchaseIntents.complete(bookId, key)
      pendingKey.value = null
    }
  } finally {
    buying.value = false
  }
}

async function restartIntent() {
  try {
    await ElMessageBox.confirm('将放弃当前重试键并创建一次全新的购买意图。仅在确认旧请求不会成功时使用。', '开始新的购买意图', {
      confirmButtonText: '确认新建', cancelButtonText: '继续保留', type: 'warning',
    })
  } catch {
    return
  }
  pendingKey.value = purchaseIntents.restart(bookId)
  purchaseError.value = ''
  ElMessage.info('已创建新的购买意图')
}

onMounted(load)
</script>

<template>
  <section class="page-width detail-page">
    <PageFeedback :loading="loading" :error="error" @retry="load" />
    <template v-if="book && !loading && !error">
      <div class="detail-gallery">
        <el-carousel v-if="book.images.length" height="540px" indicator-position="outside" :autoplay="false">
          <el-carousel-item v-for="image in book.images" :key="image.id">
            <img class="detail-image" :src="assetUrl(image.image_url)" :alt="`${book.title} 实物图`" />
          </el-carousel-item>
        </el-carousel>
        <BookCover v-else :alt="book.title" />
      </div>
      <article class="detail-copy">
        <span class="eyebrow">{{ conditionLabels[book.condition] }} · 实物图 {{ book.images.length }} 张</span>
        <h1>{{ book.title }}</h1>
        <div class="detail-price"><small>转让价</small><strong>¥{{ book.price }}</strong></div>
        <div class="detail-description">
          <h3>卖家描述</h3>
          <p>{{ book.description || '卖家暂未补充描述，可结合实物图片判断书况。' }}</p>
        </div>
        <div class="seller-note">{{ book.seller_username ? `由 ${book.seller_username} 发布` : '卖家发布' }} · 微信联系，协商交易</div>
        <el-alert v-if="purchaseError" :title="purchaseError" type="error" :closable="false" show-icon />
        <el-alert v-if="pendingKey && !order" title="已保留本次购买意图；重试会继续使用同一个幂等键。" type="warning" :closable="false" />
        <el-alert v-if="order" :title="`订单 ${order.order_no} 创建成功`" type="success" :closable="false" show-icon>
          <template #default><el-button link type="success" @click="router.push({ name: 'orders' })">查看订单，卖家交付后确认收书 →</el-button></template>
        </el-alert>
        <el-dialog v-model="contactVisible" class="seller-contact-dialog" title="联系卖家" width="min(420px, 90vw)" append-to-body align-center destroy-on-close>
          <div class="seller-contact-content">
            <p class="seller-contact-description">使用微信扫描二维码添加卖家，协商书况、价格和交付。</p>
            <el-image v-if="book.wechat_qr_code_url" class="wechat-qr-code seller-contact-qr" :src="assetUrl(book.wechat_qr_code_url)" :preview-src-list="[assetUrl(book.wechat_qr_code_url)]" preview-teleported fit="contain" alt="卖家的微信联系二维码">
              <template #error><p role="alert">二维码加载失败，请关闭弹窗后重试。</p></template>
            </el-image>
            <p v-else class="seller-contact-empty">卖家尚未补充微信联系二维码。</p>
          </div>
          <template #footer><el-button class="seller-contact-close" type="primary" @click="contactVisible = false">关闭</el-button></template>
        </el-dialog>
        <div class="detail-actions">
          <el-button type="primary" size="large" @click="contactVisible = true">联系卖家</el-button>
          <el-button size="large" :loading="buying" :disabled="isOwnBook || Boolean(order)" @click="buy">
            {{ isOwnBook ? '这是你发布的图书' : pendingKey ? '重试本次购买' : '预约购买' }}
          </el-button>
          <el-button size="large" :loading="favoriteBusy" @click="addFavorite">加入收藏</el-button>
        </div>
        <el-button v-if="pendingKey && purchaseError" link class="restart-intent" @click="restartIntent">确认旧请求无效，开始新的购买意图</el-button>
      </article>
    </template>
  </section>
</template>
