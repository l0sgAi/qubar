<template>
  <NModal
    v-model:show="showModal"
    :mask-closable="false"
    preset="card"
    :style="{ width: '600px', borderRadius: '24px' }"
    :title="t('circle.form.createButton')"
    header-style="font-size: 28px;"
    :segmented="{ content: 'soft' }"
    :bordered="false"
    size="huge"
  >
    <!-- 步骤指示器 -->
    <NSteps :current="currentStep + 1" size="small" class="steps-bar">
      <NStep :title="t('circle.form.step1Title')" />
      <NStep :title="t('circle.form.step2Title')" />
      <NStep :title="t('circle.form.step3Title')" />
      <NStep :title="t('circle.form.step4Title')" />
    </NSteps>

    <NForm
      ref="formRef"
      :model="formData"
      :rules="rules"
      label-placement="top"
      require-mark-placement="right"
      class="step-form"
      size="large"
    >
      <!-- 步骤 1：名称、标识、简介 -->
      <div v-show="currentStep === 0" class="step-pane">
        <NFormItem :label="t('circle.form.name')" path="name">
          <NInput
            v-model:value="formData.name"
            :placeholder="t('circle.form.namePlaceholder')"
            maxlength="50"
            show-count
            @input="handleNameInput"
          />
        </NFormItem>

        <NFormItem :label="t('circle.form.slug')" path="slug">
          <NInput
            v-model:value="formData.slug"
            :placeholder="t('circle.form.slugPlaceholder')"
            maxlength="60"
          />
        </NFormItem>

        <NFormItem :label="t('circle.form.description')" path="description">
          <NInput
            v-model:value="formData.description"
            type="textarea"
            :placeholder="t('circle.form.descriptionPlaceholder')"
            :autosize="{ minRows: 4, maxRows: 6 }"
            maxlength="2000"
            show-count
          />
        </NFormItem>
      </div>

      <!-- 步骤 2：图片上传 + 圈内规则 -->
      <div v-show="currentStep === 1" class="step-pane">
        <NFormItem :label="t('circle.form.avatar')" path="avatar_url">
          <NUpload
            :max="1"
            :file-list="avatarFileList"
            @update:file-list="handleAvatarChange"
            :custom-request="handleUploadAvatar"
            list-type="image-card"
            accept="image/*"
          >
            {{ t('circle.form.avatarUpload') }}
          </NUpload>
          <template #feedback>
            <NText depth="3" style="font-size: 12px">{{ t('circle.form.avatarTip') }}</NText>
          </template>
        </NFormItem>

        <NFormItem :label="t('circle.form.cover')" path="cover_url">
          <NUpload
            :max="1"
            :file-list="coverFileList"
            @update:file-list="handleCoverChange"
            :custom-request="handleUploadCover"
            list-type="image-card"
            accept="image/*"
          >
            {{ t('circle.form.avatarUpload') }}
          </NUpload>
          <template #feedback>
            <NText depth="3" style="font-size: 12px">{{ t('circle.form.coverTip') }}</NText>
          </template>
        </NFormItem>

        <NDivider />

        <NFormItem :label="t('circle.form.rules')" path="rule">
          <NInput
            v-model:value="formData.rule"
            type="textarea"
            :placeholder="t('circle.form.rulesPlaceholder')"
            :autosize="{ minRows: 4, maxRows: 8 }"
            maxlength="2000"
            show-count
          />
        </NFormItem>
      </div>

      <!-- 步骤 3：分类 + 加入方式 -->
      <div v-show="currentStep === 2" class="step-pane">
        <NFormItem :label="t('circle.form.category')" path="category_id">
          <NSelect
            v-model:value="formData.category_id"
            :options="categoryOptions"
            :loading="loadingCategories"
            :placeholder="t('circle.form.categoryPlaceholder')"
            filterable
          />
        </NFormItem>

        <NFormItem :label="t('circle.form.joinType')" path="join_type">
          <NRadioGroup v-model:value="formData.join_type" name="join_type">
            <NSpace vertical :size="12">
              <NRadio :value="0">
                <NText strong>{{ t('circle.form.joinTypeDirect') }}</NText>
                <NText depth="3" style="margin-left: 8px">{{ t('circle.form.joinTypeDirectDesc') }}</NText>
              </NRadio>
              <NRadio :value="2">
                <NText strong>{{ t('circle.form.joinTypePrivate') }}</NText>
                <NText depth="3" style="margin-left: 8px">{{ t('circle.form.joinTypePrivateDesc') }}</NText>
              </NRadio>
            </NSpace>
          </NRadioGroup>
        </NFormItem>
      </div>

      <!-- 步骤 4：确认预览（复用圈子详情页头部样式） -->
      <div v-show="currentStep === 3" class="step-pane">
        <CircleHeaderPreview
          :cover-url="formData.cover_url"
          :avatar-url="formData.avatar_url"
          :name="formData.name"
          :slug="formData.slug"
          :banner-aspect="COVER_ASPECT_RATIO"
        />
        <div class="review-summary">
          <div class="review-row">
            <span class="review-label">{{ t('circle.form.description') }}</span>
            <span class="review-value">{{ formData.description || '-' }}</span>
          </div>
          <div class="review-row">
            <span class="review-label">{{ t('circle.form.rules') }}</span>
            <span class="review-value">{{ formData.rule || '-' }}</span>
          </div>
          <div class="review-row">
            <span class="review-label">{{ t('circle.form.category') }}</span>
            <span class="review-value">{{ selectedCategoryLabel }}</span>
          </div>
          <div class="review-row">
            <span class="review-label">{{ t('circle.form.joinType') }}</span>
            <span class="review-value">{{ joinTypeText }}</span>
          </div>
        </div>
      </div>
    </NForm>

    <template #footer>
      <NSpace justify="space-between" align="center" :size="16">
        <NButton size="large" @click="handleCancel">{{ t('common.cancel') }}</NButton>
        <NSpace :size="12">
          <NButton v-if="currentStep > 0" size="large" @click="handlePrev">
            {{ t('circle.form.prevStep') }}
          </NButton>
          <NButton
            v-if="currentStep < totalSteps - 1"
            type="primary"
            size="large"
            @click="handleNext"
          >
            {{ t('circle.form.nextStep') }}
          </NButton>
          <NButton
            v-if="currentStep === totalSteps - 1"
            type="primary"
            size="large"
            :loading="loading"
            @click="handleSubmit"
          >
            {{ t('circle.form.createButton') }}
          </NButton>
        </NSpace>
      </NSpace>
    </template>
  </NModal>

  <!-- 图片裁剪弹窗（头像 1:1 / 封面 3:1） -->
  <ImageCropperModal
    v-model:show="cropperVisible"
    :image-src="cropperImageSrc"
    :aspect-ratio="cropperAspectRatio"
    :title="cropperTitle"
    :type="cropperType"
    :circle-name="formData.name"
    :cover-url="formData.cover_url"
    :avatar-url="formData.avatar_url"
    :slug="formData.slug"
    :cover-aspect="COVER_ASPECT_RATIO"
    :upload-handler="handleCropUpload"
    @confirm="handleCropConfirm"
    @cancel="handleCropCancel"
  />
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import {
  NModal,
  NForm,
  NFormItem,
  NInput,
  NButton,
  NSpace,
  NUpload,
  NRadioGroup,
  NRadio,
  NSelect,
  NSteps,
  NStep,
  NDivider,
  NText,
  useMessage
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { getCategories, createCircle } from '@/api/circle'
import { useImageUpload } from '@/composables/useImageUpload'
import ImageCropperModal from '@/components/common/ImageCropperModal.vue'
import CircleHeaderPreview from '@/components/common/CircleHeaderPreview.vue'

const { t } = useI18n()

const props = defineProps({
  show: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['update:show', 'success'])

const message = useMessage()
const formRef = ref(null)
const loading = ref(false)
const { uploadOne } = useImageUpload()
const showModal = computed({
  get: () => props.show,
  set: (val) => emit('update:show', val)
})

// 步骤控制
const totalSteps = 4
const currentStep = ref(0)
// 每一步"下一步"时需要校验的字段（其余字段暂不校验）
const stepFieldsMap = [
  ['name', 'slug', 'description'],
  ['rule'],
  ['category_id'],
  []
]

// 文件列表
const avatarFileList = ref([])
const coverFileList = ref([])

// 封面裁剪比例 ≈ CircleDetail 头部宽高比（横幅，真实约 7:1，取 6 兼顾可裁剪性）。
// 与 CircleHeaderPreview 的 BANNER_ASPECT 保持一致，确保「裁剪即所见」。
const COVER_ASPECT_RATIO = 6

// 图片裁剪弹窗状态
const cropperVisible = ref(false)
const cropperImageSrc = ref('')
// pendingCrop 缓存当前裁剪上下文：type / NUpload 的 file 与 onFinish/onError
const pendingCrop = ref(null)
const cropperAspectRatio = computed(() => (pendingCrop.value?.type === 'cover' ? COVER_ASPECT_RATIO : 1))
const cropperTitle = computed(() =>
  pendingCrop.value?.type === 'cover'
    ? t('circle.form.cropCoverTitle')
    : t('circle.form.cropAvatarTitle')
)
// 裁剪类型透传给裁剪弹窗，用于效果预览映射（头像/封面）
const cropperType = computed(() => pendingCrop.value?.type || 'avatar')

// 表单数据
const formData = ref({
  name: '',
  slug: '',
  avatar_url: '',
  cover_url: '',
  description: '',
  rule: '',
  category_id: null,
  join_type: 0
})

// 分类选项
const categoryOptions = ref([])
const loadingCategories = ref(false)
// 第 4 步汇总：分类名称
const selectedCategoryLabel = computed(() => {
  const opt = categoryOptions.value.find(o => o.value === formData.value.category_id)
  return opt ? opt.label : t('common.notSet')
})
// 第 4 步汇总：加入方式文案
const joinTypeText = computed(() => {
  const map = {
    0: t('circle.form.joinTypeDirect'),
    2: t('circle.form.joinTypePrivate')
  }
  return map[formData.value.join_type] || '-'
})

// 加载分类列表
const loadCategories = async () => {
  try {
    loadingCategories.value = true
    const res = await getCategories()
    if (res.code === 200 && res.data) {
      // 将分类数据转换为选项格式
      categoryOptions.value = res.data.map(cat => ({
        label: cat.name,
        value: cat.id
      }))
      console.log('分类列表加载成功:', categoryOptions.value)
    }
  } catch (error) {
    console.error('加载分类失败:', error)
    message.error(t('circle.form.messages.loadCategoriesFailed'))
  } finally {
    loadingCategories.value = false
  }
}

// 表单验证规则
// 每条规则注入 `key`（字段名）：handleNext 调用 validate 的 shouldRuleBeApplied
// 过滤器只拿到 rule 对象本身（naive-ui 不会注入字段名），需靠这里的 key 按字段过滤。
const rules = computed(() => {
  const raw = {
    name: [
      { required: true, message: t('circle.form.validation.nameRequired'), trigger: 'blur' },
      { min: 2, max: 50, message: t('circle.form.validation.nameLength'), trigger: 'blur' }
    ],
    slug: [
      {
        pattern: /^[a-z0-9-]*$/,
        message: t('circle.form.validation.slugPattern'),
        trigger: 'blur'
      },
      { max: 60, message: t('circle.form.validation.slugMaxLength'), trigger: 'blur' }
    ],
    description: [
      { required: true, message: t('circle.form.validation.descriptionRequired'), trigger: 'blur' },
      { min: 10, max: 2000, message: t('circle.form.validation.descriptionLength'), trigger: 'blur' }
    ],
    rule: [
      { required: true, message: t('circle.form.validation.rulesRequired'), trigger: 'blur' },
      { min: 10, max: 2000, message: t('circle.form.validation.rulesLength'), trigger: 'blur' }
    ],
    category_id: [
      { required: true, message: t('circle.form.validation.categoryRequired'), trigger: 'change' }
    ]
  }
  // 注入字段名，供按步校验的过滤器识别
  Object.entries(raw).forEach(([field, list]) => list.forEach((r) => { r.key = field }))
  return raw
})

// 名称输入处理，自动生成 slug
const handleNameInput = () => {
  if (formData.value.name && !formData.value.slug) {
    formData.value.slug = formData.value.name
      .toLowerCase()
      .replace(/[^a-z0-9一-龥]/g, '-')
      .replace(/-+/g, '-')
      .replace(/^-|-$/g, '')
  }
}

// 处理头像 file-list 同步
const handleAvatarChange = (options) => {
  avatarFileList.value = options
}

// 处理封面 file-list 同步
const handleCoverChange = (options) => {
  coverFileList.value = options
}

// 打开裁剪弹窗（选图后不直接上传，先裁剪）
const openCropper = (type, file, onFinish, onError) => {
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
  }
  pendingCrop.value = { type, file, onFinish, onError }
  cropperImageSrc.value = URL.createObjectURL(file.file)
  cropperVisible.value = true
}

const handleUploadAvatar = ({ file, onFinish, onError }) => {
  openCropper('avatar', file, onFinish, onError)
}

const handleUploadCover = ({ file, onFinish, onError }) => {
  openCropper('cover', file, onFinish, onError)
}

// 裁剪确认 → 在弹窗内上传裁剪结果（由 ImageCropperModal 调用）
const handleCropUpload = async (blob, { onProgress } = {}) => {
  const p = pendingCrop.value
  const croppedFile = new File([blob], `cropped-${p.type}.jpg`, { type: 'image/jpeg' })
  try {
    const url = await uploadOne(croppedFile, { onProgress })
    message.success(p.type === 'avatar'
      ? t('circle.form.messages.avatarUploadSuccess')
      : t('circle.form.messages.coverUploadSuccess'))
    return url
  } catch (error) {
    console.error('裁剪后上传失败:', error)
    message.error(t('circle.form.messages.uploadFailedRetry'))
    throw error // rethrow → 弹窗保持打开，供重试
  }
}

// 上传成功 → 回填 URL 并结束本次裁剪
const handleCropConfirm = (payload) => {
  const p = pendingCrop.value
  if (!p) return
  const url = payload?.url
  if (!url) return
  if (p.type === 'avatar') {
    formData.value.avatar_url = url
  } else {
    formData.value.cover_url = url
  }
  p.onFinish()
  pendingCrop.value = null
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
    cropperImageSrc.value = ''
  }
}

// 取消裁剪 → 放弃本次上传
const handleCropCancel = () => {
  const p = pendingCrop.value
  if (p) p.onError()
  pendingCrop.value = null
  if (cropperImageSrc.value.startsWith('blob:')) {
    URL.revokeObjectURL(cropperImageSrc.value)
    cropperImageSrc.value = ''
  }
}

// 下一步：只校验当前步的字段，通过才前进
const handleNext = async () => {
  const fields = stepFieldsMap[currentStep.value]
  try {
    if (fields.length) {
      await formRef.value?.validate(undefined, (rule) => !!rule.key && fields.includes(rule.key))
    }
    if (currentStep.value < totalSteps - 1) currentStep.value++
  } catch {
    // 校验失败：naive-ui 已在当前步字段下显示错误，停留本步并给出提示
    message.warning(t('circle.form.validation.pleaseComplete'))
  }
}

// 上一步
const handlePrev = () => {
  if (currentStep.value > 0) currentStep.value--
}

// 根据字段名定位所属步骤索引（提交校验失败时，用于跳转到出错步骤）
const findStepByField = (field) => {
  const idx = stepFieldsMap.findIndex((fields) => fields.includes(field))
  return idx === -1 ? null : idx
}

// 提交表单
const handleSubmit = async () => {
  // 最终全量校验（安全兜底；前面各步已逐步校验过）
  try {
    await formRef.value?.validate()
  } catch (errors) {
    // 校验失败：前面步骤的字段被 v-show 隐藏，红字错误用户在提交页看不到。
    // 故弹出全局提示，并自动跳转到第一个出错字段所在的步骤，让红字错误可见。
    message.warning(t('circle.form.validation.pleaseComplete'))
    const firstField = errors && typeof errors === 'object' ? Object.keys(errors)[0] : null
    const step = firstField ? findStepByField(firstField) : null
    if (step !== null) currentStep.value = step
    return
  }

  try {
    loading.value = true

    // 调用创建兴趣圈接口
    const res = await createCircle(formData.value)
    console.log('创建结果:' + res)
    if (res.code === 200) {
      message.success(t('circle.form.messages.createSuccess'))
      emit('success', res.data)
      handleReset()
      showModal.value = false
    } else {
      message.error(res.message || t('circle.form.messages.createFailed'))
    }
  } catch (error) {
    console.error('创建兴趣圈失败:', error)
    // 仅在此处才可能是接口/网络错误，优先显示接口返回的错误消息
    const errorMsg = error.message || t('circle.form.messages.createNetworkError')
    message.error(errorMsg)
  } finally {
    loading.value = false
  }
}

// 取消
const handleCancel = () => {
  showModal.value = false
  handleReset()
}

// 重置表单
const handleReset = () => {
  setTimeout(() => {
    formRef.value?.restoreValidation()
    currentStep.value = 0
    formData.value = {
      name: '',
      slug: '',
      avatar_url: '',
      cover_url: '',
      description: '',
      rule: '',
      category_id: null,
      join_type: 0
    }
    avatarFileList.value = []
    coverFileList.value = []
  }, 200)
}

watch(showModal, (val) => {
  if (val) {
    // 弹窗打开时加载分类列表
    loadCategories()
  } else {
    handleReset()
  }
})
</script>

<style scoped>
/* 步骤指示器 */
.steps-bar {
  padding: 4px 8px 20px;
}

/* 表单区域：常规屏幕无需滚动，矮屏兜底可滚（滚动条隐藏，保持视觉干净） */
.step-form {
  max-height: 70vh;
  overflow-y: auto;
  padding: 0 8px;
  /* Firefox */
  scrollbar-width: none;
  /* IE / 旧版 Edge */
  -ms-overflow-style: none;
}

/* Chrome / Safari / 新版 Edge：隐藏滚动条 */
.step-form::-webkit-scrollbar {
  display: none;
}

/* 每步面板给一个最小高度，避免步骤切换时弹窗高度跳动 */
.step-pane {
  min-height: 340px;
}

/* 第 4 步：确认预览汇总 */
.review-summary {
  margin-top: 16px;
  display: flex;
  flex-direction: column;
}
.review-row {
  display: flex;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}
.review-row:last-child {
  border-bottom: none;
}
.review-label {
  width: 80px;
  flex-shrink: 0;
  color: rgba(255, 255, 255, 0.5);
  font-size: 0.9rem;
}
.review-value {
  color: rgba(255, 255, 255, 0.85);
  font-size: 0.9rem;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}

/* ===== 卡片整体：圆润 + 柔和阴影 ===== */
:deep(.n-card) {
  border-radius: 24px !important;
  box-shadow: var(--shadow-lg) !important;
  background: var(--glass-bg) !important;
  border: 1px solid var(--glass-border) !important;
}

:deep(.n-card-header) {
  border-radius: 24px 24px 0 0 !important;
}

:deep(.n-card-header__title) {
  font-size: 26px !important;
  font-weight: 700 !important;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

/* ===== 输入框 / 选择框：更高、更圆 ===== */
:deep(.n-input),
:deep(.n-base-selection) {
  --n-border-radius: 14px !important;
  --n-height: 52px !important;
  font-size: 15px;
}

:deep(.n-base-selection-label) {
  min-height: 52px !important;
}

:deep(.n-input__textarea-el) {
  font-size: 15px;
  line-height: 1.7;
}

/* 聚焦态：绿色边框 + 柔光晕 */
:deep(.n-input--focus),
:deep(.n-base-selection:focus-within) {
  --n-border-focus: 1px solid #18a058 !important;
  --n-box-shadow-focus: 0 0 0 3px rgba(24, 160, 88, 0.18) !important;
}

/* ===== 按钮：圆角 + 高度，主按钮绿色渐变 ===== */
:deep(.n-button) {
  --n-border-radius: 14px !important;
  --n-height: 46px !important;
  transition: all 0.2s ease;
}

:deep(.n-button--primary-type) {
  background: var(--primary-gradient) !important;
  border: none !important;
  color: #07140d !important;
  font-weight: 600 !important;
}

:deep(.n-button--primary-type:hover) {
  transform: translateY(-1px);
  box-shadow: 0 8px 20px rgba(34, 179, 106, 0.35);
  opacity: 0.95;
}

/* ===== 图片上传卡片：圆润化 ===== */
:deep(.n-upload-trigger--image-card),
:deep(.n-upload-file-list__item) {
  border-radius: 16px !important;
  transition: all 0.2s ease;
}

:deep(.n-upload-trigger--image-card:hover) {
  border-color: #18a058 !important;
  color: #18a058 !important;
}

/* ===== 表单标签：略增间距，更通透 ===== */
:deep(.n-form-item) {
  margin-bottom: 4px;
}

</style>
