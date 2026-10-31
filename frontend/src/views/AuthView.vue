<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { ApiError, errorMessage } from '@/api/http'
import { api } from '@/api'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const mode = ref<'login' | 'register'>('login')
const formRef = ref<FormInstance>()
const error = ref('')
const submitting = ref(false)
const form = reactive({ identifier: '', username: '', email: '', password: '', emailCode: '' })
const sendingCode = ref(false)
const resendSeconds = ref(0)
let countdown: ReturnType<typeof setInterval> | undefined
watch(() => form.email, () => { form.emailCode = '' })
onUnmounted(() => clearInterval(countdown))
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
  emailCode: [
    { required: true, message: '请输入邮箱验证码', trigger: 'blur' },
    { pattern: /^[0-9]{6}$/, message: '请输入 6 位数字验证码', trigger: 'blur' },
  ],
  password: [
    { required: true, message: '请输入密码', trigger: 'blur' },
    { min: 8, max: 72, message: '密码长度为 8–72 位', trigger: 'blur' },
  ],
}

async function submit() {
  if (submitting.value || sendingCode.value) return
  submitting.value = true
  try {
    if (!await formRef.value?.validate().catch(() => false)) return
    error.value = ''
    const password = form.password
    if (mode.value === 'register') {
      const registered = await auth.register(form.username, form.email, password, form.emailCode)
      try {
        await auth.login(registered.email, password)
      } catch (cause) {
        form.identifier = registered.email
        form.password = ''
        form.emailCode = ''
        switchMode('login')
        error.value = `账号已创建，但自动登录未完成，请直接登录。${errorMessage(cause)}`
        return
      }
      ElMessage.success('注册成功，已自动登录')
    } else {
      await auth.login(form.identifier, password)
      ElMessage.success('登录成功')
    }
    form.password = ''
    form.emailCode = ''
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
    await router.replace(redirect)
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    submitting.value = false
  }
}

function startCountdown(seconds: number) {
  clearInterval(countdown)
  const until = Date.now() + seconds * 1000
  resendSeconds.value = seconds
  countdown = setInterval(() => {
    resendSeconds.value = Math.max(0, Math.ceil((until - Date.now()) / 1000))
    if (resendSeconds.value === 0) clearInterval(countdown)
  }, 1000)
}

async function sendCode() {
  if (sendingCode.value || resendSeconds.value > 0) return
  if (!await formRef.value?.validateField('email').then(() => true).catch(() => false)) return
  sendingCode.value = true
  error.value = ''
  try {
    const result = await api.requestRegistrationCode(form.email)
    startCountdown(result.resend_after)
    ElMessage.success('验证码已发送，请查收邮件；5 分钟内有效')
  } catch (cause) {
    error.value = errorMessage(cause)
    if (cause instanceof ApiError && cause.code === 'EMAIL_CODE_RATE_LIMITED') {
      const retryAfter = (cause.details as { retry_after?: unknown } | undefined)?.retry_after
      if (typeof retryAfter === 'number' && retryAfter > 0) startCountdown(retryAfter)
    }
  } finally {
    sendingCode.value = false
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
      <span class="eyebrow light">A BOOKSHELF FOR EVERY READER</span>
      <h1>一本书的价值，<br />不止被读一次。</h1>
      <p>每一位爱读书的人，都可以在这里发现好书、联系卖家，或把闲置书交给下一位读者。</p>
      <div class="story-stat"><strong>1–6</strong><span>张实物图<br />看清每处细节</span></div>
    </div>
    <div class="auth-card">
      <span class="eyebrow">BOOKLOOP ACCOUNT</span>
      <h2>{{ title }}</h2>
      <p>{{ mode === 'login' ? '继续发现好书，与更多读者相遇。' : '创建账号，找到想读的书，分享读过的好书。' }}</p>
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
        <el-form-item v-if="mode === 'login'" label="用户名或邮箱" prop="identifier">
          <el-input v-model="form.identifier" autocomplete="username" />
        </el-form-item>
        <template v-else>
          <el-form-item label="用户名" prop="username"><el-input v-model="form.username" autocomplete="username" /></el-form-item>
          <el-form-item label="邮箱" prop="email"><el-input v-model="form.email" autocomplete="email" :disabled="sendingCode" /></el-form-item>
          <el-form-item label="邮箱验证码" prop="emailCode">
            <div class="email-code-row">
              <el-input v-model="form.emailCode" maxlength="6" inputmode="numeric" autocomplete="one-time-code" placeholder="6 位数字" />
              <el-button native-type="button" :loading="sendingCode" :disabled="resendSeconds > 0 || submitting" @click="sendCode">
                {{ resendSeconds > 0 ? `${resendSeconds} 秒后重发` : '获取验证码' }}
              </el-button>
            </div>
          </el-form-item>
        </template>
        <el-form-item label="密码" prop="password">
          <el-input v-model="form.password" type="password" show-password :autocomplete="mode === 'login' ? 'current-password' : 'new-password'" />
        </el-form-item>
        <el-button class="full-button" type="primary" native-type="submit" :loading="submitting" :disabled="sendingCode">
          {{ mode === 'login' ? '登录' : '创建账号' }}
        </el-button>
      </el-form>
      <div class="auth-switch">
        {{ mode === 'login' ? '还没有账号？' : '已经有账号？' }}
        <el-button link type="primary" :disabled="submitting" @click="switchMode(mode === 'login' ? 'register' : 'login')">
          {{ mode === 'login' ? '立即注册' : '返回登录' }}
        </el-button>
      </div>
      <small class="security-note">令牌仅保存在当前浏览器标签会话中，请勿在公共电脑保持登录。</small>
    </div>
  </section>
</template>

<style scoped>
.email-code-row { display: flex; gap: 10px; width: 100%; }
.email-code-row .el-input { min-width: 0; }
.email-code-row .el-button { flex-shrink: 0; }
</style>

