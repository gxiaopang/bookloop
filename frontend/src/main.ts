import { createApp } from 'vue'
import { createPinia } from 'pinia'
import {
  ElAlert,
  ElButton,
  ElCarousel,
  ElCarouselItem,
  ElDialog,
  ElForm,
  ElFormItem,
  ElImage,
  ElInput,
  ElInputNumber,
  ElOption,
  ElPagination,
  ElSegmented,
  ElSelect,
  ElSkeleton,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus'
import 'element-plus/dist/index.css'
import App from './App.vue'
import router from './router'
import './styles.css'

const app = createApp(App)
app.use(createPinia())
app.use(router)
for (const component of [
  ElAlert,
  ElButton,
  ElCarousel,
  ElCarouselItem,
  ElDialog,
  ElForm,
  ElFormItem,
  ElImage,
  ElInput,
  ElInputNumber,
  ElOption,
  ElPagination,
  ElSegmented,
  ElSelect,
  ElSkeleton,
  ElTable,
  ElTableColumn,
  ElTag,
]) {
  app.use(component)
}
app.mount('#app')
