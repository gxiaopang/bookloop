<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { api } from '@/api'
import { errorMessage } from '@/api/http'
import type { BookCondition, BookListItem, Pagination } from '@/types/api'
import BookCard from '@/components/BookCard.vue'
import PageFeedback from '@/components/PageFeedback.vue'

const books = ref<BookListItem[]>([])
const pagination = ref<Pagination>({ page: 1, page_size: 12, total: 0, total_pages: 0 })
const loading = ref(false)
const error = ref('')
const filters = reactive<{
  keyword: string
  condition: BookCondition | ''
  sort: 'newest' | 'price_asc' | 'price_desc'
}>({ keyword: '', condition: '', sort: 'newest' })

async function load(page = 1) {
  loading.value = true
  error.value = ''
  try {
    const result = await api.listBooks({ ...filters, page, page_size: 12 })
    books.value = result.items
    pagination.value = result.pagination
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

function reset() {
  filters.keyword = ''
  filters.condition = ''
  filters.sort = 'newest'
  void load()
}

onMounted(() => load())
</script>

<template>
  <div>
    <section class="hero">
      <div class="hero-copy">
        <span class="eyebrow light">BOOKS DESERVE ANOTHER CHAPTER</span>
        <h1>让读过的书，<br /><em>继续被需要。</em></h1>
        <p>知识不毕业，好书再循环。</p>
      </div>
      <div class="hero-art" aria-hidden="true">
        <span class="orbit orbit-one"></span>
        <span class="orbit orbit-two"></span>
        <div class="hero-book">
          <div class="hero-book-kicker"><span>SECOND CHAPTER</span><b>01</b></div>
          <strong class="hero-book-title"><span>BOOK</span><span>LOOP</span></strong>
          <div class="hero-book-footer">
            <i>校园书市</i>
            <small>READ · SHARE · REPEAT</small>
          </div>
        </div>
      </div>
    </section>

    <section class="market-section page-width">
      <div class="section-intro">
        <div><span class="eyebrow">CAMPUS SHELF</span><h2>正在流动的好书</h2></div>
        <span class="result-count">{{ pagination.total }} 本在售</span>
      </div>

      <form class="filter-bar" @submit.prevent="load()">
        <el-input v-model="filters.keyword" clearable maxlength="100" placeholder="按书名搜索" class="search-input" />
        <el-select v-model="filters.condition" placeholder="全部品相">
          <el-option label="全部品相" value="" />
          <el-option label="全新" value="new" />
          <el-option label="近全新" value="like_new" />
          <el-option label="品相良好" value="good" />
          <el-option label="有使用痕迹" value="fair" />
        </el-select>
        <el-select v-model="filters.sort" aria-label="排序方式">
          <el-option label="最新发布" value="newest" />
          <el-option label="价格从低到高" value="price_asc" />
          <el-option label="价格从高到低" value="price_desc" />
        </el-select>
        <el-button native-type="submit" type="primary" :loading="loading">搜索</el-button>
        <el-button @click="reset">重置</el-button>
      </form>

      <PageFeedback :loading="loading" :error="error" :empty="!books.length" empty-title="没有找到符合条件的图书" @retry="load(pagination.page)" />
      <div v-if="!loading && !error && books.length" class="book-grid">
        <BookCard v-for="book in books" :key="book.id" :book="book" />
      </div>
      <el-pagination
        v-if="pagination.total_pages > 1"
        class="pagination"
        layout="prev, pager, next"
        :current-page="pagination.page"
        :page-size="pagination.page_size"
        :total="pagination.total"
        @current-change="load"
      />
    </section>
  </div>
</template>
