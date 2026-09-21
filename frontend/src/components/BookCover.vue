<script setup lang="ts">
import { computed, ref } from 'vue'
import { assetUrl } from '@/api'

const props = defineProps<{ src?: string; alt: string }>()
const failed = ref(false)
const resolved = computed(() => assetUrl(props.src))
</script>

<template>
  <div class="book-cover">
    <img v-if="resolved && !failed" :src="resolved" :alt="alt" loading="lazy" @error="failed = true" />
    <div v-else class="cover-placeholder" aria-hidden="true">
      <span>BOOK</span>
      <i></i>
      <small>LOOP</small>
    </div>
  </div>
</template>

