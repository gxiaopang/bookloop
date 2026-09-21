<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { FormInstance, FormRules } from 'element-plus'
import { api, assetUrl } from '@/api'
import { errorMessage } from '@/api/http'
import type { Book, BookCondition, BookImage } from '@/types/api'
import PageFeedback from '@/components/PageFeedback.vue'
import PageHeader from '@/components/PageHeader.vue'

const route = useRoute()
const router = useRouter()
const editId = route.name === 'book-edit' ? Number(route.params.id) : null
const formRef = ref<FormInstance>()
const fileInput = ref<HTMLInputElement>()
const book = ref<Book | null>(null)
const loading = ref(Boolean(editId))
const saving = ref(false)
const uploading = ref(false)
const imageBusy = ref(false)
const publishing = ref(false)
const error = ref('')
const form = reactive<{ title: string; description: string; price: string; condition: BookCondition }>({
  title: '', description: '', price: '', condition: 'good',
})
const rules: FormRules = {
  title: [{ required: true, message: '请输入书名', trigger: 'blur' }, { max: 200, message: '书名最多 200 字', trigger: 'blur' }],
  price: [
    { required: true, message: '请输入价格', trigger: 'blur' },
    { pattern: /^(0|[1-9][0-9]{0,7})(\.[0-9]{1,2})?$/, message: '请输入大于 0、最多两位小数的价格', trigger: 'blur' },
  ],
  condition: [{ required: true, message: '请选择品相', trigger: 'change' }],
  description: [{ max: 10000, message: '描述最多 10000 字', trigger: 'blur' }],
}
const pageTitle = computed(() => editId ? '编辑图书' : book.value ? '补充实物图片' : '发布一本书')
const canPublish = computed(() => book.value?.status === 'off_shelf' && Boolean(book.value.images.length))

async function load() {
  if (!editId || !Number.isInteger(editId) || editId < 1) {
    error.value = '图书编号无效'
    loading.value = false
    return
  }
  loading.value = true
  error.value = ''
  try {
    const result = await api.getMyBook(editId)
    book.value = result
    fillForm(result)
  } catch (cause) {
    error.value = errorMessage(cause)
  } finally {
    loading.value = false
  }
}

function fillForm(value: Book) {
  form.title = value.title
  form.description = value.description || ''
  form.price = value.price
  form.condition = value.condition
}

async function saveDetails() {
  if (!await formRef.value?.validate().catch(() => false)) return
  saving.value = true
  try {
    const payload = { ...form }
    book.value = book.value
      ? await api.updateBook(book.value.id, payload)
      : await api.createBook(payload)
    fillForm(book.value)
    ElMessage.success(editId ? '图书信息已保存' : '资料已创建，请继续上传实物图片')
    if (!editId) await router.replace({ name: 'book-edit', params: { id: book.value.id } })
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    saving.value = false
  }
}

function chooseImages() {
  fileInput.value?.click()
}

async function uploadSelected(event: Event) {
  const input = event.target as HTMLInputElement
  const files = [...(input.files || [])]
  input.value = ''
  if (!book.value || !files.length) return
  const slots = 6 - book.value.images.length
  if (files.length > slots) {
    ElMessage.warning(`当前最多还能上传 ${slots} 张图片`)
    return
  }
  uploading.value = true
  try {
    for (const file of files) {
      if (!['image/jpeg', 'image/png'].includes(file.type)) throw new Error(`${file.name} 不是 JPEG 或 PNG 图片`)
      const created = await api.uploadBookImage(book.value.id, file)
      book.value.images.push(created)
    }
    ElMessage.success(`已上传 ${files.length} 张图片`)
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    uploading.value = false
  }
}

async function removeImage(image: BookImage) {
  if (!book.value) return
  try {
    await ElMessageBox.confirm('确认删除这张实物图片？', '删除图片', { type: 'warning' })
  } catch {
    return
  }
  imageBusy.value = true
  try {
    await api.deleteBookImage(book.value.id, image.id)
    book.value.images = book.value.images.filter((item) => item.id !== image.id)
    ElMessage.success('图片已删除')
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    imageBusy.value = false
  }
}

async function moveImage(index: number, direction: -1 | 1) {
  if (!book.value) return
  const target = index + direction
  if (target < 0 || target >= book.value.images.length) return
  const next = [...book.value.images]
  ;[next[index], next[target]] = [next[target]!, next[index]!]
  imageBusy.value = true
  try {
    book.value.images = await api.reorderBookImages(book.value.id, next.map((image) => image.id))
    ElMessage.success('图片顺序已更新')
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    imageBusy.value = false
  }
}

async function publish() {
  if (!book.value || !canPublish.value) return
  try {
    await ElMessageBox.confirm('上架后图书会立即出现在公开书市，确认继续？', '上架图书', { type: 'success' })
  } catch {
    return
  }
  publishing.value = true
  try {
    book.value = await api.putBookOnShelf(book.value.id)
    ElMessage.success('图书已上架')
    await router.push({ name: 'my-books' })
  } catch (cause) {
    ElMessage.error(errorMessage(cause))
  } finally {
    publishing.value = false
  }
}

onMounted(() => { if (editId) void load() })
</script>

<template>
  <section class="page-width inner-page editor-page">
    <PageHeader eyebrow="SELL A BOOK" :title="pageTitle" description="先保存图书资料，再上传 1–6 张实物图片；确认顺序后才能上架。">
      <RouterLink :to="{ name: 'my-books' }"><el-button>返回我的发布</el-button></RouterLink>
    </PageHeader>
    <PageFeedback :loading="loading" :error="error" @retry="load" />
    <div v-if="!loading && !error" class="editor-layout">
      <section class="editor-card">
        <div class="step-heading"><span>01</span><div><h2>图书资料</h2><p>价格和描述应与实物情况一致。</p></div></div>
        <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="saveDetails">
          <el-form-item label="书名" prop="title"><el-input v-model="form.title" maxlength="200" show-word-limit /></el-form-item>
          <div class="form-grid">
            <el-form-item label="转让价（元）" prop="price"><el-input v-model="form.price" placeholder="例如 28.00" /></el-form-item>
            <el-form-item label="品相" prop="condition">
              <el-select v-model="form.condition">
                <el-option label="全新" value="new" /><el-option label="近全新" value="like_new" />
                <el-option label="品相良好" value="good" /><el-option label="有使用痕迹" value="fair" />
              </el-select>
            </el-form-item>
          </div>
          <el-form-item label="实物描述" prop="description"><el-input v-model="form.description" type="textarea" :rows="6" maxlength="10000" show-word-limit placeholder="版本、笔记、折痕或缺页等信息请如实说明" /></el-form-item>
          <el-button type="primary" native-type="submit" :loading="saving">{{ book ? '保存资料' : '创建下架商品并继续' }}</el-button>
        </el-form>
      </section>

      <section class="editor-card" :class="{ muted: !book }">
        <div class="step-heading"><span>02</span><div><h2>实物图片</h2><p>第一张是列表封面，可用箭头调整顺序。</p></div></div>
        <template v-if="book">
          <input ref="fileInput" class="visually-hidden" type="file" multiple accept="image/jpeg,image/png" @change="uploadSelected" />
          <div class="image-manager">
            <article v-for="(image, index) in book.images" :key="image.id" class="managed-image">
              <img :src="assetUrl(image.image_url)" :alt="`实物图 ${index + 1}`" />
              <span v-if="index === 0">封面</span>
              <div>
                <el-button text :disabled="index === 0 || imageBusy" aria-label="前移" @click="moveImage(index, -1)">←</el-button>
                <el-button text :disabled="index === book.images.length - 1 || imageBusy" aria-label="后移" @click="moveImage(index, 1)">→</el-button>
                <el-button text type="danger" :disabled="imageBusy" @click="removeImage(image)">删除</el-button>
              </div>
            </article>
            <button v-if="book.images.length < 6" class="upload-tile" type="button" :disabled="uploading" @click="chooseImages">
              <strong>{{ uploading ? '上传中…' : '+' }}</strong><span>添加图片</span><small>{{ book.images.length }}/6</small>
            </button>
          </div>
          <el-alert v-if="!book.images.length" title="至少上传一张图片后才能上架" type="warning" :closable="false" show-icon />
        </template>
        <p v-else class="locked-step">完成并保存图书资料后即可上传图片。</p>
      </section>

      <section class="editor-card publish-card" :class="{ muted: !book }">
        <div class="step-heading"><span>03</span><div><h2>确认上架</h2><p>上架前再次核对信息和封面顺序。</p></div></div>
        <template v-if="book">
          <el-alert v-if="book.status === 'available'" title="当前图书已在公开书市展示；资料修改会立即生效。" type="success" :closable="false" />
          <el-button v-else type="primary" size="large" :disabled="!canPublish" :loading="publishing" @click="publish">确认上架</el-button>
        </template>
      </section>
    </div>
  </section>
</template>
