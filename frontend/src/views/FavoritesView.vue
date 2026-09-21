<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import { errorMessage } from '@/api/http'
import type { FavoriteItem, Pagination } from '@/types/api'
import { bookStatusLabels, conditionLabels, statusType } from '@/utils/presentation'
import BookCover from '@/components/BookCover.vue'
import PageFeedback from '@/components/PageFeedback.vue'
import PageHeader from '@/components/PageHeader.vue'

const items = ref<FavoriteItem[]>([])
const pagination = ref<Pagination>({ page: 1, page_size: 12, total: 0, total_pages: 0 })
const loading = ref(false)
const error = ref('')
const removing = ref<number | null>(null)

async function load(page = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await api.listFavorites({ page, page_size: 12 })
    items.value = result.items
    pagination.value = result.pagination
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

async function remove(bookId: number) {
  removing.value = bookId
  try {
    await api.removeFavorite(bookId)
    items.value = items.value.filter((item) => item.book_id !== bookId)
    pagination.value.total--
    ElMessage.success('已取消收藏')
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    removing.value = null
  }
}

onMounted(() => load())
</script>

<template>
  <section class="page-width inner-page">
    <PageHeader eyebrow="SAVED BOOKS" title="我的收藏" description="即使图书状态变化，收藏仍会保留，方便你识别和整理。" />
    <PageFeedback :loading="loading" :error="error" :empty="!items.length" empty-title="收藏夹还是空的" empty-text="去书市遇见一本想读的书吧。" @retry="load(pagination.page)">
      <RouterLink :to="{ name: 'market' }"><el-button type="primary">去逛书市</el-button></RouterLink>
    </PageFeedback>
    <div v-if="!loading && !error && items.length" class="favorite-list">
      <article v-for="item in items" :key="item.book_id" class="favorite-row">
        <RouterLink :to="item.is_available ? { name: 'book-detail', params: { id: item.book_id } } : ''">
          <BookCover :src="item.cover_image_url" :alt="item.title" />
        </RouterLink>
        <div class="favorite-copy">
          <span class="eyebrow">{{ conditionLabels[item.condition] }}</span>
          <h3>{{ item.title }}</h3>
          <strong>¥{{ item.price }}</strong>
          <el-tag :type="statusType(item.status)" effect="plain">{{ bookStatusLabels[item.status] }}</el-tag>
        </div>
        <div class="favorite-actions">
          <RouterLink v-if="item.is_available" :to="{ name: 'book-detail', params: { id: item.book_id } }"><el-button type="primary">查看</el-button></RouterLink>
          <el-button :loading="removing === item.book_id" @click="remove(item.book_id)">移除</el-button>
        </div>
      </article>
    </div>
    <el-pagination v-if="pagination.total_pages > 1" class="pagination" layout="prev, pager, next" :current-page="pagination.page" :page-size="pagination.page_size" :total="pagination.total" @current-change="load" />
  </section>
</template>

