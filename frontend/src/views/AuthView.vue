<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { errorMessage } from '@/api/http'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const mode = ref<'login' | 'register'>('login')
const formRef = ref<FormInstance>()
const error = ref('')
const form = reactive({ identifier: '', username: '', email: '', password: '' })
const title = computed(() => mode.value === 'login' ? '欢迎回来' : '加入 BookLoop')
const rules: FormRules = {
  identifier: [{ required: true, message: '请输入用户名或邮箱', trigger: 'blur' }],
  username: [
    { required: true, message: '请输入用户名', trigger: 'blur' },
    { pattern: /^[A-Za-z0-9_]{3,32}$/, message: '3–32 位字母、数字或下划线', trigger: 'blur' },
  ],
  email: [
    { required: true, message: '请输入邮箱', trigger: 'blur' },
    { type: 'email', message: '邮箱格式不正确', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 8, max: 72, message: '密码长度为 8–72 位', trigger: 'blur' },
  ],
}

async function submit() {
  if (!await formRef.value?.validate().catch(() => false)) return
  error.value = ''
  try {
    if (mode.value === 'register') {
      await auth.register(form.username, form.email, form.password)
      ElMessage.success('注册成功，请使用新账号登录')
      form.identifier = form.username
      form.password = ''
      switchMode('login')
      return
    }
    await auth.login(form.identifier, form.password)
    ElMessage.success('登录成功')
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    await router.replace(redirect)
  } catch (cause) {
    error.value = errorMessage(cause)
  }
}

function switchMode(next: 'login' | 'register') {
  mode.value = next
  error.value = ''
  formRef.value?.clearValidate()
}
</script>

<template>
  <section class="auth-page page-width">
    <div class="auth-story">
      <span class="eyebrow light">YOUR CAMPUS BOOKSHELF</span>
      <h1>一本书的价值，<br />不止被读一次。</h1>
      <p>登录后收藏心仪图书、发起购买，或把闲置书交给下一位读者。</p>
      <div class="story-stat"><strong>1–6</strong><span>张实物图<br />看清每处细节</span></div>
    </div>
    <div class="auth-card">
      <span class="eyebrow">BOOKLOOP ACCOUNT</span>
      <h2>{{ title }}</h2>
      <p>{{ mode === 'login' ? '继续你的校园书籍循环。' : '创建账号，开始第一笔书籍循环。' }}</p>
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
        <el-form-item v-if="mode === 'login'" label="用户名或邮箱" prop="identifier">
          <el-input v-model="form.identifier" autocomplete="username" />
        </el-form-item>
        <template v-else>
          <el-form-item label="用户名" prop="username"><el-input v-model="form.username" autocomplete="username" /></el-form-item>
          <el-form-item label="邮箱" prop="email"><el-input v-model="form.email" autocomplete="email" /></el-form-item>
        </template>
        <el-form-item label="密码" prop="password">
          <el-input v-model="form.password" type="password" show-password :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" />
        </el-form-item>
        <el-button class="full-button" type="primary" native-type="submit" :loading="auth.loading">
          {{ mode === 'login' ? '登录' : '创建账号' }}
        </el-button>
      </el-form>
      <div class="auth-switch">
        {{ mode === 'login' ? '还没有账号？' : '已经有账号？' }}
        <el-button link type="primary" @click="switchMode(mode === 'login' ? 'register' : 'login')">
          {{ mode === 'login' ? '立即注册' : '返回登录' }}
        </el-button>
      </div>
      <small class="security-note">令牌仅保存在当前浏览器标签会话中，请勿在公共电脑保持登录。</small>
    </div>
  </section>
</template>

