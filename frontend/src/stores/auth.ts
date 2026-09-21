import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/api'
import { authSession } from '@/services/authSession'
import type { User } from '@/types/api'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<User | null>(authSession.getUser())
  const loading = ref(false)

  // Read the reactive user first. If the non-reactive token is checked first,
  // an initially empty session short-circuits before Vue can track user.value,
  // leaving this computed value cached as false after a successful login.
  const isAuthenticated = computed(() => Boolean(user.value && authSession.getToken()))
  const isAdmin = computed(() => user.value?.role === 'admin')

  async function login(identifier: string, password: string) {
    loading.value = true
    try {
      const result = await api.login({ identifier, password })
      authSession.save(result.access_token, result.user)
      user.value = result.user
    } finally {
      loading.value = false
    }
  }

  async function register(username: string, email: string, password: string) {
    loading.value = true
    try {
      return await api.register({ username, email, password })
    } finally {
      loading.value = false
    }
  }

  async function refreshUser() {
    if (!authSession.getToken()) return
    const current = await api.currentUser()
    authSession.updateUser(current)
    user.value = current
  }

  function logout() {
    authSession.clear()
    user.value = null
  }

  function resetAfterUnauthorized() {
    user.value = null
  }

  return { user, loading, isAuthenticated, isAdmin, login, register, refreshUser, logout, resetAfterUnauthorized }
})
