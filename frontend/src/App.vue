<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '@/stores/auth'

const auth = useAuthStore()
const router = useRouter()
const route = useRoute()

function logout() {
  auth.logout()
  ElMessage.success('已安全退出当前浏览器会话')
  void router.push({ name: 'market' })
}

function handleUnauthorized() {
  auth.resetAfterUnauthorized()
  ElMessage.warning('登录已失效，请重新登录')
  void router.push({ name: 'auth', query: { redirect: route.fullPath } })
}

onMounted(() => window.addEventListener('bookloop:unauthorized', handleUnauthorized))
onBeforeUnmount(() => window.removeEventListener('bookloop:unauthorized', handleUnauthorized))
</script>

<template>
  <div class="app-shell">
    <header class="site-header">
      <RouterLink class="brand" :to="{ name: 'market' }">
        <span class="brand-mark">B</span>
        <span><strong>BookLoop</strong><small>好书循环，共享阅读</small></span>
      </RouterLink>
      <nav class="main-nav" aria-label="主导航">
        <RouterLink :to="{ name: 'market' }">逛书市</RouterLink>
        <template v-if="auth.isAuthenticated">
          <RouterLink :to="{ name: 'favorites' }">收藏</RouterLink>
          <RouterLink :to="{ name: 'orders' }">订单</RouterLink>
          <RouterLink :to="{ name: 'my-books' }">卖书</RouterLink>
          <RouterLink v-if="auth.isAdmin" :to="{ name: 'admin' }">管理</RouterLink>
        </template>
      </nav>
      <div class="account-actions">
        <template v-if="auth.isAuthenticated">
          <div class="user-pill">
            <span class="user-avatar" aria-hidden="true">{{ auth.user?.username?.slice(0, 1).toUpperCase() || 'U' }}</span>
            <span class="welcome">{{ auth.user?.username }}</span>
          </div>
          <el-button text @click="logout">退出</el-button>
        </template>
        <RouterLink v-else :to="{ name: 'auth', query: { redirect: route.fullPath } }">
          <el-button class="login-button" round>登录 / 注册</el-button>
        </RouterLink>
      </div>
    </header>

    <main class="site-main">
      <RouterView v-slot="{ Component }">
        <Transition name="page" mode="out-in">
          <component :is="Component" />
        </Transition>
      </RouterView>
    </main>

    <footer class="site-footer">
      <div class="footer-inner">
        <div class="footer-brand">
          <strong>BookLoop</strong>
          <span>让闲置好书，遇见下一位读者。</span>
        </div>
        <div class="footer-meta">
          <span>实物多图展示</span>
          <span>·</span>
          <span>微信扫码联系</span>
        </div>
      </div>
    </footer>
  </div>
</template>

