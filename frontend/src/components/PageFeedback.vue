<script setup lang="ts">
withDefaults(defineProps<{
  loading?: boolean
  error?: string
  empty?: boolean
  emptyTitle?: string
  emptyText?: string
}>(), {
  loading: false,
  error: '',
  empty: false,
  emptyTitle: '这里还没有内容',
  emptyText: '换个条件试试，或稍后再来看看。',
})

defineEmits<{ retry: [] }>()
</script>

<template>
  <div v-if="loading" class="feedback-card" aria-live="polite">
    <el-skeleton :rows="4" animated />
  </div>
  <div v-else-if="error" class="feedback-card error-state" role="alert">
    <span class="feedback-symbol">!</span>
    <h3>没有加载成功</h3>
    <p>{{ error }}</p>
    <el-button @click="$emit('retry')">重新加载</el-button>
  </div>
  <div v-else-if="empty" class="feedback-card empty-state">
    <span class="feedback-symbol">○</span>
    <h3>{{ emptyTitle }}</h3>
    <p>{{ emptyText }}</p>
    <slot />
  </div>
</template>

