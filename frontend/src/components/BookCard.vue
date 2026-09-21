<script setup lang="ts">
import type { BookListItem } from '@/types/api'
import { conditionLabels } from '@/utils/presentation'
import BookCover from './BookCover.vue'

defineProps<{ book: BookListItem }>()
</script>

<template>
  <RouterLink class="book-card" :to="{ name: 'book-detail', params: { id: book.id } }">
    <BookCover :src="book.cover_image_url" :alt="book.title" />
    <div class="book-card-body">
      <span class="eyebrow">{{ conditionLabels[book.condition] }}</span>
      <h3>{{ book.title }}</h3>
      <div class="book-card-footer">
        <strong>¥{{ book.price }}</strong>
        <span>查看详情 →</span>
      </div>
    </div>
  </RouterLink>
</template>

