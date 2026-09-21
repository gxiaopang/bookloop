import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

declare module 'vue-router' {
  interface RouteMeta {
    requiresAuth?: boolean
    guestOnly?: boolean
    adminOnly?: boolean
    title?: string
  }
}

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  scrollBehavior: () => ({ top: 0 }),
  routes: [
    { path: '/', name: 'market', component: () => import('@/views/MarketView.vue'), meta: { title: '逛书市' } },
    { path: '/books/:id', name: 'book-detail', component: () => import('@/views/BookDetailView.vue'), meta: { title: '图书详情' } },
    { path: '/auth', name: 'auth', component: () => import('@/views/AuthView.vue'), meta: { guestOnly: true, title: '登录与注册' } },
    { path: '/favorites', name: 'favorites', component: () => import('@/views/FavoritesView.vue'), meta: { requiresAuth: true, title: '我的收藏' } },
    { path: '/orders', name: 'orders', component: () => import('@/views/OrdersView.vue'), meta: { requiresAuth: true, title: '我的订单' } },
    { path: '/sell', name: 'my-books', component: () => import('@/views/MyBooksView.vue'), meta: { requiresAuth: true, title: '我的发布' } },
    { path: '/sell/new', name: 'book-new', component: () => import('@/views/BookEditorView.vue'), meta: { requiresAuth: true, title: '发布图书' } },
    { path: '/sell/:id/edit', name: 'book-edit', component: () => import('@/views/BookEditorView.vue'), meta: { requiresAuth: true, title: '编辑图书' } },
    { path: '/admin', name: 'admin', component: () => import('@/views/AdminView.vue'), meta: { requiresAuth: true, adminOnly: true, title: '管理台' } },
    { path: '/:pathMatch(.*)*', name: 'not-found', component: () => import('@/views/NotFoundView.vue'), meta: { title: '页面不存在' } },
  ],
})

router.beforeEach((to) => {
  const auth = useAuthStore()
  document.title = `${to.meta.title || 'BookLoop'} · BookLoop`
  if (to.meta.requiresAuth && !auth.isAuthenticated) {
    return { name: 'auth', query: { redirect: to.fullPath } }
  }
  if (to.meta.adminOnly && !auth.isAdmin) return { name: 'market' }
  if (to.meta.guestOnly && auth.isAuthenticated) return { name: 'market' }
})

export default router

