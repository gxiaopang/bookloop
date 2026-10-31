<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { errorMessage } from '@/api/http'
import type { BookListItem, Pagination } from '@/types/api'
import { bookStatusLabels, conditionLabels, statusType } from '@/utils/presentation'
import BookCover from '@/components/BookCover.vue'
import PageFeedback from '@/components/PageFeedback.vue'
import PageHeader from '@/components/PageHeader.vue'

const books = ref<BookListItem[]>([])
const pagination = ref<Pagination>({ page: 1, page_size: 12, total: 0, total_pages: 0 })
const loading = ref(false)
const error = ref('')
const acting = ref<number | null>(null)

async function load(page = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await api.listMyBooks({ page, page_size: 12 })
    books.value = result.items
    pagination.value = result.pagination
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

async function toggleShelf(book: BookListItem) {
  const next = book.status === 'available' ? '下架' : '上架'
  try {
    await ElMessageBox.confirm(`确认${next}《${book.title}》？`, `${next}图书`, { type: 'warning' })
  } catch {
    return
  }
  acting.value = book.id
  try {
    const updated = book.status === 'available' ? await api.takeBookOffShelf(book.id) : await api.putBookOnShelf(book.id)
    book.status = updated.status
    ElMessage.success(`已${next}`)
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    acting.value = null
  }
}

onMounted(() => load())
</script>

<template>
  <section class="page-width inner-page">
    <PageHeader eyebrow="YOUR SHELF" title="我的发布" description="新图书默认下架；上传实物图和微信联系二维码后才能上架。">
      <RouterLink :to="{ name: 'book-new' }"><el-button type="primary" size="large">发布一本书</el-button></RouterLink>
    </PageHeader>
    <PageFeedback :loading="loading" :error="error" :empty="!books.length" empty-title="还没有发布图书" empty-text="从一本闲置好书开始你的 BookLoop。" @retry="load(pagination.page)">
      <RouterLink :to="{ name: 'book-new' }"><el-button type="primary">发布第一本书</el-button></RouterLink>
    </PageFeedback>
    <div v-if="!loading && !error && books.length" class="my-book-grid">
      <article v-for="book in books" :key="book.id" class="my-book-card">
        <BookCover :src="book.cover_image_url" :alt="book.title" />
        <div class="my-book-copy">
          <div><span class="eyebrow">{{ conditionLabels[book.condition] }}</span><el-tag :type="statusType(book.status)" effect="plain">{{ bookStatusLabels[book.status] }}</el-tag></div>
          <h3>{{ book.title }}</h3>
          <strong>¥{{ book.price }}</strong>
          <div class="my-book-actions">
            <RouterLink v-if="['available', 'off_shelf'].includes(book.status)" :to="{ name: 'book-edit', params: { id: book.id } }"><el-button>编辑与联系方式</el-button></RouterLink>
            <el-button v-if="['available', 'off_shelf'].includes(book.status)" class="shelf-toggle" type="primary" :plain="book.status === 'available'" :loading="acting === book.id" @click="toggleShelf(book)">
              {{ book.status === 'available' ? '下架' : '上架' }}
            </el-button>
          </div>
        </div>
      </article>
    </div>
    <el-pagination v-if="pagination.total_pages > 1" class="pagination" layout="prev, pager, next" :current-page="pagination.page" :page-size="pagination.page_size" :total="pagination.total" @current-change="load" />
  </section>
</template>
