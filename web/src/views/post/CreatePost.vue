<template>
  <div class="create-post-page">
    <!-- 顶栏 -->
    <AppHeader />

    <!-- 侧边栏 -->
    <SideNav @collapsed="offset = 64" @expanded="offset = 260" />

    <!-- 主内容区域 -->
    <div class="main-content" :style="{ 'margin-left': `${offset}px`, width: `calc(100% - ${offset}px)` }">
      <div class="create-post-container">
        <div class="content-wrapper">
          <NCard
          :title="t('post.createPost')"
          :bordered="false"
          class="post-card"
          :header-style="{fontSize:'35px',marginLeft:'20px',marginTop:'20px'}"
          >
            <NForm
              ref="formRef"
              :model="formData"
              :rules="rules"
              label-placement="top"
              require-mark-placement="right-hanging"
            >
              <!-- 圈子选择 -->
              <NFormItem :label="t('post.belongToCircle')" path="circle_id">
                <NSelect
                  v-model:value="formData.circle_id"
                  :options="circleOptions"
                  :loading="loadingCircles"
                  filterable
                  :placeholder="t('post.selectCircle')"
                  clearable
                  @search="handleSearchCircle"
                  @update:value="handleCircleChange"
                  :render-label="renderCircleLabel"
                  size="large"
                  class="circle-select"
                />
              </NFormItem>

              <!-- 标题 -->
              <NFormItem :label="t('post.title')" path="title">
                <NInput
                  v-model:value="formData.title"
                  :placeholder="t('post.titlePlaceholder')"
                  maxlength="200"
                  show-count
                  size="large"
                  class="title-input"
                />
              </NFormItem>

              <!-- 摘要 -->
              <!-- <NFormItem label="摘要（检索关键词，可选）" path="summary">
                <NInput
                  v-model:value="formData.summary"
                  type="textarea"
                  placeholder="请输入摘要，用于检索和关键词匹配"
                  maxlength="500"
                  show-count
                  size="large"
                  class="title-input"
                />
              </NFormItem> -->

              <!-- 正文（Markdown编辑器）：编辑器 + 图片墙 + 底栏包在同一容器内，与评论框一致 -->
              <NFormItem :label="t('post.content')" path="content">
                <div class="editor-wrapper">
                  <MdEditor
                    ref="postEditorRef"
                    v-model="formData.content"
                    :language="language"
                    :preview="false"
                    :toolbars="toolbars"
                    theme="dark"
                    :placeholder="t('post.contentPlaceholder')"
                    :max-length="50000"
                    :rows="25"
                    :footers="[]"
                  />
                  <!-- 底栏：自定义图片上传 + @提及 -->
                  <div class="editor-footer">
                    <input
                      ref="fileInputRef"
                      type="file"
                      accept="image/*"
                      multiple
                      style="display: none"
                      :aria-label="t('comment.editor.uploadImage')"
                      @change="handleFileSelect"
                    />
                    <NButton
                      quaternary
                      :title="t('comment.editor.uploadImage')"
                      :disabled="contentImageCount >= MAX_POST_IMAGES || uploading"
                      @click="fileInputRef.click()"
                    >
                      <template #icon>
                        <NIcon size="20">
                          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                            <circle cx="8.5" cy="8.5" r="1.5"></circle>
                            <polyline points="21 15 16 10 5 21"></polyline>
                          </svg>
                        </NIcon>
                      </template>
                    </NButton>
                    <!-- @提及：编辑器内输入 @ 触发选人；圈内置顶/正文 @选人按所属圈子过滤可见机器人 -->
                        <MentionTrigger
                      :get-editor-view="getPostEditorView"
                      :selected-ids="selectedIds"
                      :circle-id="formData.circle_id"
                      @select="recordSelection"
                    />
                  </div>
                </div>
              </NFormItem>
              <div class="footer-actions">
                <div class="footer-buttons">
                  <NButton size="large" @click="handleCancel">{{ t('common.cancel') }}</NButton>
                  <NButton
                    size="large"
                    type="primary"
                    :loading="submitting"
                    :disabled="uploading"
                    @click="handleSubmit"
                  >
                    {{ t('common.submit') }}
                  </NButton>
                </div>
              </div>
            </NForm>
          </NCard>

          <!-- 右侧规则卡片 -->
          <CircleRuleCard :circle-data="selectedCircleData" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, h } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  NCard,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NButton,
  NIcon,
  NAvatar,
  NText,
  useMessage
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { MdEditor } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import AppHeader from '@/components/layout/AppHeader.vue'
import SideNav from '@/components/layout/SideNav.vue'
import CircleRuleCard from '@/components/post/CircleRuleCard.vue'
import { getMyCircles, createPost } from '@/api/post'
import { getCircleDetail } from '@/api/circle'
import { useImageUpload } from '@/composables/useImageUpload'
import MentionTrigger from '@/components/post/MentionTrigger.vue'
import { useMentions } from '@/composables/useMentions'
import { ensureMentionHighlight } from '@/utils/mentionHighlight'
import { filterMentionedIds } from '@/utils/mention'

const router = useRouter()
const route = useRoute()
const message = useMessage()
const { t } = useI18n()
const { uploading, uploadMany } = useImageUpload({ withProgress: true })
// 图片上传走自定义 file input，成功后以 md 语法插入光标处并自动开预览
const fileInputRef = ref(null)
const MAX_POST_IMAGES = 9
// 图片数从正文实时统计（删了正文里的图即释放名额）
const contentImageCount = computed(() => extractImageUrls(formData.value.content).length)
const offset = ref(260)
const formRef = ref(null)
const submitting = ref(false)
const loadingCircles = ref(false)
const language = ref('zh-CN')
let searchTimer = null

// 选中的圈子数据
const selectedCircleData = ref(null)

// @提及：按钮选人与正文内输入 @ 触发选人，共用同一份已选列表与 10 人上限；
// 提交时 filterMentionedIds 过滤出正文中仍存在的 token 对应 uuid 传后端
const postEditorRef = ref(null)
const getPostEditorView = () => {
  const view = postEditorRef.value?.getEditorView?.()
  if (view) ensureMentionHighlight(view)
  return view
}
// 表单数据（须先于 useMentions：其内部 watch 创建时会同步调一次 getText）
const formData = ref({
  circle_id: null,
  title: '',
  summary: '',
  content: '',
  media_extra: []
})

const { mentionedUsers, selectedIds, recordSelection, appendAtEnd } = useMentions({
  getText: () => formData.value.content,
  setText: v => { formData.value.content = v }
})

// 圈子选项
const circleOptions = ref([])

// 编辑器工具栏配置
const toolbars = [
  'bold',
  'underline',
  'italic',
  '-',
  'title',
  'strikeThrough',
  // 'sub',
  // 'sup',
  'quote',
  'unorderedList',
  'orderedList',
  'task',
  '-',
  'codeRow',
  'code',
  'link',
  'table',
  // 'katex',
  // 'mermaid',
  '-',
  'revoke',
  'next',
  'save',
  '=',
  'pageFullscreen',
  'fullscreen',
  'preview',
  'catalog'
]

// 表单验证规则
const rules = computed(() => ({
  circle_id: {
    required: true,
    message: t('post.validation.circleRequired'),
    trigger: ['change', 'blur']
  },
  title: {
    required: true,
    message: t('post.validation.titleRequired'),
    trigger: ['blur', 'input']
  },
  content: {
    required: true,
    message: t('post.validation.contentRequired'),
    trigger: ['blur', 'input']
  }
}))

// 加载用户已加入的圈子列表
const loadCircles = async (keyword = '') => {
  try {
    loadingCircles.value = true
    const res = await getMyCircles({
      keyword,
      size: 50
    })

    if (res.data && res.data.circles) {
      circleOptions.value = res.data.circles.map(circle => ({
        label: circle.name,
        value: circle.id,
        avatar: circle.avatar_url,
        member_count: circle.member_count
      }))
    }
  } catch (error) {
    console.error('加载圈子列表失败:', error)
    message.error(t('post.loadCirclesFailed'))
  } finally {
    loadingCircles.value = false
  }
}

// 搜索圈子（带防抖）
const handleSearchCircle = (keyword) => {
  // 清除之前的定时器
  if (searchTimer) {
    clearTimeout(searchTimer)
  }

  // 如果没有输入，加载所有圈子
  if (!keyword) {
    loadCircles('')
    return
  }

  // 设置新的定时器，2秒后执行搜索
  searchTimer = setTimeout(() => {
    loadCircles(keyword)
  }, 2000)
}

// 自定义圈子选项渲染
const renderCircleLabel = (option) => {
  return h('div', { style: { display: 'flex', alignItems: 'center', gap: '12px', padding: '8px 0' } }, [
    h(NAvatar, {
      size: 48,
      src: option.avatar || undefined,
      round: true
    }, {
      default: () => !option.avatar ? (option.label?.charAt(0) || '?') : undefined
    }),
    h('div', { style: { flex: 1 } }, [
      h('div', { style: { fontWeight: 500, fontSize: '21px' } }, option.label),
      h(NText, {
        depth: 3,
        style: { fontSize: '16px' }
      }, { default: () => `${option.member_count || 0} ${t('circle.members')}` })
    ])
  ])
}

// 圈子选择变化时加载圈子详情
const handleCircleChange = async (circleId) => {
  if (!circleId) {
    selectedCircleData.value = null
    return
  }

  try {
    const res = await getCircleDetail(circleId)
    if (res.data) {
      selectedCircleData.value = res.data
    }
  } catch (error) {
    console.error('加载圈子详情失败:', error)
  }
}

// 从 Markdown 内容中提取所有图片 URL
const extractImageUrls = (content) => {
  const imageRegex = /!\[.*?\]\((.*?)\)/g
  const urls = []
  let match
  while ((match = imageRegex.exec(content)) !== null) {
    urls.push(match[1])
  }
  return urls
}

// 自定义图片上传：成功后以 md 语法插入光标处，并打开预览供检查
const handleFileSelect = (e) => {
  const files = Array.from(e.target.files || [])
  if (!files.length) return
  uploadFiles(files)
  e.target.value = ''
}

const uploadFiles = async (files) => {
  if (contentImageCount.value >= MAX_POST_IMAGES) {
    message.warning(t('upload.maxImages', { max: MAX_POST_IMAGES }))
    return
  }
  const remaining = MAX_POST_IMAGES - contentImageCount.value
  const filesToUpload = files.slice(0, remaining)
  if (filesToUpload.length < files.length) {
    message.warning(t('upload.exceedLimit', { remaining }))
  }
  try {
    const urls = await uploadMany(filesToUpload)
    // 上传期间用户可能已删/增图，插入前按实时剩余名额截断
    const remainingNow = MAX_POST_IMAGES - contentImageCount.value
    const urlsToInsert = urls.slice(0, Math.max(0, remainingNow))
    if (!urlsToInsert.length) {
      message.warning(t('upload.maxImages', { max: MAX_POST_IMAGES }))
      return
    }
    // 逐张插入 ![](url)，插入后光标落在末尾
    const mdText = urlsToInsert.map(url => `![image](${url})`).join('\n') + '\n'
    postEditorRef.value?.insert(() => ({
      targetValue: mdText,
      select: false,
      deviationStart: 0,
      deviationEnd: 0
    }))
    // 打开预览供检查（重复调用幂等：status 显式传 true）
    postEditorRef.value?.togglePreview(true)
    message.success(t('upload.success'))
  } catch (error) {
    console.error('图片上传失败:', error)
    message.error(t('upload.failed'))
  }
}

// 提交表单
const handleSubmit = async () => {
  // 图片上传未完成时禁止提交，避免正文里出现未上传的占位
  if (uploading.value) return
  try {
    await formRef.value?.validate()

    // 从内容中提取实际的图片 URL，更新 media_extra；发布前再校验一次图片上限
    const actualUrls = extractImageUrls(formData.value.content)
    if (actualUrls.length > MAX_POST_IMAGES) {
      message.warning(t('upload.maxImages', { max: MAX_POST_IMAGES }))
      return
    }

    submitting.value = true
    // 正文里被删掉的 @ 不传；须为完整 @用户名 token（@alice 不命中 @alice2）；
    // 后端对重复/@自己/不存在用户会静默过滤，最多生效 10 人
    const mentionIds = filterMentionedIds(formData.value.content, mentionedUsers.value)
    const submitData = {
      ...formData.value,
      media_extra: actualUrls,
      mention_user_ids: mentionIds.length ? mentionIds : undefined
    }

    const res = await createPost(submitData)

    // 跳转到帖子详情页：新帖可能先进入审核（status=2），接口不返回 status，
    // 由详情页拉到实际状态后提示「发布成功」或「已提交审核」（?justCreated=1）
    if (res.data) {
      const postId = res.data
      router.push({
        name: 'post-detail',
        params: { id: postId },
        query: { justCreated: '1' }
      })
    } else {
      message.warning(t('post.publishNoId'))
      router.push('/home')
    }
  } catch (error) {
    console.error('发布失败:', error)
    if (error.errors) {
      // 表单验证错误
      return
    }
    message.error(error.message || t('post.publishFailed'))
  } finally {
    submitting.value = false
  }
}

// 取消
const handleCancel = () => {
  router.back()
}

onMounted(async () => {
  await loadCircles()

  // 检查 URL 参数中是否有 circleId，如果有则默认选中
  const circleIdFromUrl = route.query.circleId
  if (circleIdFromUrl) {
    const circleId = circleIdFromUrl
    // 检查圈子是否在选项列表中
    const circleExists = circleOptions.value.some(option => option.value === circleId)
    if (circleExists) {
      formData.value.circle_id = circleId
      // 加载圈子详情以显示规则卡片
      await handleCircleChange(circleId)
    }
  }
})
</script>

<style scoped>
/* 编辑器容器：编辑器 + 图片墙 + 底栏共享一个边框（与评论框一致） */
.editor-wrapper {
  width: 100%;
  border-radius: 18px;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.1);
}

.editor-wrapper :deep(.md-editor) {
  border: none !important;
  border-radius: 0 !important;
}

.editor-wrapper :deep(.md-editor-toolbar-wrapper) {
  border-radius: 0;
}

/* 编辑区 / 预览区分界：左边框 + 预览区微亮底色，分区一眼可辨 */
.editor-wrapper :deep(.md-editor-preview-wrapper) {
  border-left: 1px solid rgba(255, 255, 255, 0.12);
}

.editor-footer {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 10px 16px;
  background: rgb(24, 24, 28);
  border-top: 1px solid rgba(255, 255, 255, 0.06);
}

.create-post-page {
  min-height: 100vh;
  position: relative;
}

.main-content {
  margin-top: var(--header-height);
  padding: 24px;
  min-height: calc(100vh - var(--header-height));
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.create-post-container {
  width: 100%;
  max-width: 1400px;
  margin: 0 auto;
}

.content-wrapper {
  display: flex;
  gap: 24px;
  align-items: flex-start;
}

.content-wrapper .post-card {
  flex: 1;
  min-width: 0;
}

.post-card {
  border-radius: 16px !important;
  overflow: hidden;
}

:deep(.n-card__header) {
  padding: 24px 32px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
}

:deep(.n-card__content) {
  padding: 32px;
}

:deep(.n-card__footer) {
  padding: 20px 32px;
  border-top: 1px solid rgba(255, 255, 255, 0.1);
}

:deep(.n-form-item) {
  margin-bottom: 28px;
}

:deep(.n-form-item-label) {
  font-weight: 500;
  font-size: 16px;
  padding-bottom: 10px;
}

/* 圈子选择下拉框 */
.circle-select {
  max-width: 33.33%;
}

:deep(.circle-select .n-base-selection) {
  min-height: 62px !important;
}

:deep(.circle-select .n-base-selection__border),
:deep(.circle-select .n-base-selection__border--focused) {
  box-shadow: none !important;
}

:deep(.circle-select .n-base-selection-label) {
  min-height: 62px !important;
  font-size: 18px !important;
  display: flex !important;
  align-items: center !important;
}

:deep(.circle-select .n-base-selection-placeholder) {
  font-size: 18px !important;
  min-height: 62px !important;
  display: flex !important;
  align-items: center !important;
}

:deep(.circle-select .n-base-selection-input) {
  font-size: 18px !important;
}

/* 下拉列表选项样式 */
:deep(.n-virtuoso__item) {
  min-height: 56px !important;
}

:deep(.n-base-select-option__content) {
  min-height: 56px !important;
  padding: 12px 16px !important;
}

:deep(.n-input),
:deep(.n-base-selection) {
  border-radius: 12px !important;
  transition: all 0.3s ease;
}

:deep(.n-input:hover),
:deep(.n-base-selection:hover) {
  border-radius: 12px !important;
}

:deep(.n-input__input-el),
:deep(.n-input__textarea-el) {
  font-size: 18px;
}

:deep(.n-input--large .n-input__input-el),
:deep(.n-input--large .n-input__textarea-el) {
  font-size: 18px;
}

/* 标题输入框样式 */
.title-input {
  min-height: 64px;
  font-size: 18px;
  max-width: 75%;
}

:deep(.title-input .n-input__input-el) {
  height: 64px !important;
  line-height: 64px !important;
  font-size: 18px !important;
}

/* 摘要输入框样式 */
.summary-input :deep(.n-input__textarea-el) {
  font-size: 18px !important;
  line-height: 1.6;
}

/* Markdown 编辑器样式 */
:deep(.md-editor) {
  font-size: 18px !important;
  border-radius: 18px !important;
  overflow: hidden;
}

/* 强制覆盖所有内部文本:字体 */
:deep(.md-editor *) {
  font-family: 'Fira Code', 'Cascadia Code', 'Maple Mono NF CN', 'JetBrains Mono', 'Source Code Pro', 'Menlo', 'Monaco', 'Consolas', 'Courier New', 'PingFang SC', 'Microsoft YaHei', monospace;
}

:deep(.cm-scroller *){
  font-size: 18px;
}

/* 工具栏样式 */
:deep(.md-editor-toolbar) {
  font-size: 18px !important;
  border-radius: 21px 21px 0 0 !important;
  padding: 7px !important;
}

:deep(.md-editor-toolbar-item svg) {
  width: 18px !important;
  height: 18px !important;
}

:deep(.md-editor-footer){
  height: 7% !important;
}

/* 底部按钮：右对齐 取消/提交 */
.footer-actions {
  display: flex;
  justify-content: flex-end;
}

.footer-buttons {
  display: flex;
  gap: 16px;
}

.footer-buttons .n-button {
  min-width: 120px;
  border-radius: 10px;
  font-weight: 500;
}

/* md-editor-v3 深色主题自定义 — 偏黑底色 */
:deep(.md-editor-dark) {
  --md-bk-color: rgb(24, 24, 28);
  --md-scrollbar-bg-color: rgb(24, 24, 28);
}

:deep(.md-editor-dark .md-editor-preview) {
  /* 预览区比编辑区(rgb(24,24,28))微亮，配合 preview-wrapper 左边框形成明确分区 */
  --md-theme-bg-color: rgb(30, 30, 37);
  /* --md-theme-bg-color-inset: #141419;
  --md-theme-color-hover: #141419;
  --md-theme-border-color: rgba(255, 255, 255, 0.1);
  --md-theme-code-block-bg-color: #141419;
  --md-theme-table-stripe-color: #0a0a0f; */
}

/* 响应式 */
@media (max-width: 1400px) {
  .main-content {
    margin-right: 24px;
  }
}

@media (max-width: 1200px) {
  .main-content {
    margin-right: 24px;
  }

  .content-wrapper {
    flex-direction: column;
  }
}

@media (max-width: 768px) {
  .main-content {
    margin-left: 0;
    margin-right: 0;
    padding: 16px;
  }

  :deep(.n-card__header),
  :deep(.n-card__content),
  :deep(.n-card__footer) {
    padding: 20px;
  }

  .create-post-container {
    max-width: 100%;
  }
  
}
</style>
