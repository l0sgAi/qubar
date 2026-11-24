<template>
  <NModal
    :show="show"
    @update:show="(v) => emit('update:show', v)"
    preset="card"
    :style="{ width: '1040px', borderRadius: '24px' }"
    :title="title"
    :bordered="false"
    size="huge"
    :mask-closable="false"
    :segmented="{ content: 'soft' }"
  >
    <div class="crop-layout">
      <!-- 上：裁剪区 -->
      <div class="crop-layout__cropper">
        <div class="cropper-wrapper">
          <img ref="imgEl" :src="imageSrc" class="cropper-img" />
        </div>
      </div>
      <!-- 下：实时预览 -->
      <div class="crop-layout__preview">
        <div class="preview-label">{{ resolvedPreviewTitle }}</div>
        <!-- 个人信息场景：仅预览圆形头像 -->
        <div v-if="avatarOnly" class="avatar-preview">
          <NAvatar :size="120" :src="avatarPreviewSrc" round class="avatar-preview__lg" />
          <NAvatar :size="64" :src="avatarPreviewSrc" round class="avatar-preview__md" />
          <NAvatar :size="40" :src="avatarPreviewSrc" round class="avatar-preview__sm" />
        </div>
        <!-- 圈子场景：横幅铺满宽度，复用圈子详情页头部样式 -->
        <CircleHeaderPreview
          v-else
          :cover-url="previewCover"
          :avatar-url="previewAvatar"
          :name="circleName"
          :slug="slug"
          :banner-aspect="coverAspect"
        />
        <p class="preview-hint">{{ resolvedPreviewHint }}</p>
      </div>
    </div>

    <template #footer>
      <NProgress
        v-if="confirming && uploadProgress != null"
        type="line"
        :percentage="uploadProgress"
        class="crop-upload-progress"
      />
      <NSpace justify="space-between" align="center">
        <NButton quaternary round @click="handleReset">
          {{ t('circle.form.cropReset') }}
        </NButton>
        <NSpace :size="12">
          <NButton round @click="handleCancel">{{ t('common.cancel') }}</NButton>
          <NButton type="primary" round :loading="confirming" @click="handleConfirm">
            {{ t('circle.form.cropConfirm') }}
          </NButton>
        </NSpace>
      </NSpace>
    </template>
  </NModal>
</template>

<script setup>
import { ref, watch, nextTick, computed, onBeforeUnmount } from 'vue'
import { NModal, NButton, NSpace, NAvatar, NProgress, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import Cropper from 'cropperjs'
import CircleHeaderPreview from '@/components/common/CircleHeaderPreview.vue'

const props = defineProps({
  show: { type: Boolean, default: false },
  imageSrc: { type: String, default: '' },
  aspectRatio: { type: Number, default: 1 },
  title: { type: String, default: '' },
  // 效果预览相关
  type: { type: String, default: 'avatar' }, // 'avatar' | 'cover'
  circleName: { type: String, default: '' },
  coverUrl: { type: String, default: '' },
  avatarUrl: { type: String, default: '' },
  slug: { type: String, default: '' },
  // 预览头部 banner 横幅宽高比（始终用封面比例，不随当前裁剪类型变化），
  // 与 CreateCircleModal 封面裁剪比例同步，默认取真实头部比例。
  coverAspect: { type: Number, default: 6 },
  // 个人信息场景：仅预览圆形头像（不渲染圈子头部横幅）
  avatarOnly: { type: Boolean, default: false },
  // 预览标题/提示文案覆盖（空值回退到圈子默认文案）
  previewTitle: { type: String, default: '' },
  previewHint: { type: String, default: '' },
  // 可选：裁剪确认后在弹窗内直接上传。提供时 confirm 事件载荷为 { url }，
  // 且确认按钮在整个网络上传期间保持 loading；未提供时维持旧行为（emit blob）。
  // 签名：(blob, { onProgress }) => Promise<url>
  uploadHandler: { type: Function, default: null }
})
const emit = defineEmits(['update:show', 'confirm', 'cancel'])

const { t } = useI18n()
const message = useMessage()
const imgEl = ref(null)
const confirming = ref(false)
const uploadProgress = ref(null) // null=未上传；0-100=上传中进度
const liveCropDataUrl = ref('')
let cropper = null
let cropDebounceTimer = null

// 实时裁剪数据 → 预览（小尺寸 canvas，比确认上传的 2000px 更快）
const updateLivePreview = () => {
  if (!cropper) return
  try {
    const canvas = cropper.getCroppedCanvas({ maxWidth: 800, maxHeight: 800 })
    if (canvas) {
      liveCropDataUrl.value = canvas.toDataURL('image/jpeg', 0.9)
    }
  } catch (e) {
    // cropper 尚未就绪时忽略
  }
}
const debouncedUpdateLivePreview = () => {
  clearTimeout(cropDebounceTimer)
  cropDebounceTimer = setTimeout(updateLivePreview, 120)
}

// 预览用的封面/头像：当前正在裁剪的那张用实时结果，另一张用表单已有值
const previewCover = computed(() =>
  props.type === 'cover' ? liveCropDataUrl.value : props.coverUrl
)
const previewAvatar = computed(() =>
  props.type === 'avatar' ? liveCropDataUrl.value : props.avatarUrl
)

// 个人信息场景：头像预览图（实时裁剪优先，未裁剪时回退原图）
const avatarPreviewSrc = computed(() => liveCropDataUrl.value || props.avatarUrl)
// 预览标题/提示：调用方可覆盖，否则回退圈子默认文案
const resolvedPreviewTitle = computed(() => props.previewTitle || t('circle.form.previewTitle'))
const resolvedPreviewHint = computed(() => props.previewHint || t('circle.form.previewHint'))

// 初始化（或重置）裁剪实例
const initCropper = () => {
  if (cropper) {
    cropper.destroy()
    cropper = null
  }
  if (!imgEl.value || !props.imageSrc) return
  liveCropDataUrl.value = ''
  cropper = new Cropper(imgEl.value, {
    aspectRatio: props.aspectRatio,
    viewMode: 1,
    dragMode: 'move',
    autoCropArea: 0.9,
    background: false,
    responsive: true,
    restore: false,
    movable: true,
    zoomable: true,
    zoomOnTouch: true,
    zoomOnWheel: true,
    rotatable: false,
    scalable: false,
    cropBoxMovable: true,
    cropBoxResizable: true,
    toggleDragModeOnDblclick: false,
    ready: updateLivePreview,
    crop: debouncedUpdateLivePreview,
    cropend: updateLivePreview
  })
}

const destroyCropper = () => {
  clearTimeout(cropDebounceTimer)
  if (cropper) {
    cropper.destroy()
    cropper = null
  }
  liveCropDataUrl.value = ''
}

// 弹窗打开时初始化，关闭时销毁
watch(
  () => props.show,
  async (val) => {
    if (val) {
      await nextTick()
      initCropper()
    } else {
      destroyCropper()
    }
  }
)

// 同一弹窗连续裁剪不同图片（如头像→封面）时，imageSrc 变化重新初始化
watch(
  () => props.imageSrc,
  async () => {
    if (props.show) {
      await nextTick()
      initCropper()
    }
  }
)

const handleReset = () => {
  cropper?.reset()
}

const handleCancel = () => {
  emit('cancel')
  emit('update:show', false)
}

const handleConfirm = () => {
  if (!cropper) return
  const canvas = cropper.getCroppedCanvas({
    imageSmoothingEnabled: true,
    imageSmoothingQuality: 'high',
    maxWidth: 2000,
    maxHeight: 2000
  })
  if (!canvas) {
    message.error(t('circle.form.messages.cropFailed'))
    return
  }
  confirming.value = true
  canvas.toBlob(
    async (blob) => {
      if (!blob) {
        confirming.value = false
        message.error(t('circle.form.messages.cropFailed'))
        return
      }
      try {
        if (props.uploadHandler) {
          uploadProgress.value = 0
          const url = await props.uploadHandler(blob, {
            onProgress: (pct) => { uploadProgress.value = pct }
          })
          emit('confirm', { url })
        } else {
          emit('confirm', blob)
        }
        emit('update:show', false)
      } catch (e) {
        // 上传失败：保持弹窗打开供重试；错误提示由 uploadHandler 自行处理
      } finally {
        confirming.value = false
        uploadProgress.value = null
      }
    },
    'image/jpeg',
    0.92
  )
}

onBeforeUnmount(() => {
  destroyCropper()
})
</script>

<style scoped>
.crop-upload-progress {
  margin-bottom: 12px;
}

/* 上下分栏：上裁剪、下实时预览（横幅铺满宽度，预览区更大） */
.crop-layout {
  display: flex;
  flex-direction: column;
  gap: 20px;
  align-items: stretch;
}
.crop-layout__cropper {
  width: 100%;
  min-width: 0;
}
.crop-layout__preview {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 14px;
  padding: 12px 16px;
}
/* 预览横幅限宽并居中：避免在宽弹窗里被拉得过高，压缩预览区整体高度以适配小屏 */
.crop-layout__preview :deep(.circle-header) {
  max-width: 560px;
  margin: 0 auto;
}
.preview-label {
  font-size: 0.85rem;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.75);
  text-align: center;
}

/* 个人信息场景：头像多尺寸实时预览（模拟资料卡 / 导航栏 / 列表头像） */
.avatar-preview {
  display: flex;
  align-items: flex-end;
  justify-content: center;
  gap: 28px;
  padding: 18px 0 6px;
}
.avatar-preview :deep(.n-avatar) {
  border: 3px solid rgba(255, 255, 255, 0.12);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
  background: rgba(255, 255, 255, 0.06);
}

.cropper-wrapper {
  width: 100%;
  height: 360px;
  background: rgba(0, 0, 0, 0.35);
  border-radius: 14px;
  overflow: hidden;
}

.cropper-img {
  display: block;
  max-width: 100%;
  /* 初始隐藏原图，避免 cropperjs 初始化前闪现 */
  visibility: hidden;
}

/* 与 CreateCircleModal 风格统一：卡片玻璃质感 + 绿色标题 */
:deep(.n-card) {
  background: var(--glass-bg) !important;
  border: 1px solid var(--glass-border) !important;
  box-shadow: var(--shadow-lg) !important;
}

:deep(.n-card-header__title) {
  font-size: 22px !important;
  font-weight: 700 !important;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.preview-hint {
  margin: 0;
  font-size: 0.85rem;
  color: rgba(255, 255, 255, 0.55);
  line-height: 1.5;
  text-align: center;
}
</style>
