<template>
  <NCard :bordered="false" class="comment-editor-card">
    <div class="comment-editor-header">
      <span class="comment-section-title">{{ t('comment.editor.title') }}</span>
    </div>
    <!-- 访客态：隐藏编辑器，展示「登录后评论」引导 -->
    <div v-if="!isLoggedIn" class="guest-comment-prompt" @click="requireLogin('comment')">
      <NIcon size="20" :component="CommentIcon" />
      <span>{{ t('comment.editor.loginToComment') }}</span>
    </div>
    <!-- 登录态：原编辑器 -->
    <div v-else class="comment-editor-wrapper">
      <MdEditor
        ref="commentEditorRef"
        v-model="content"
        :language="language"
        :preview="false"
        :toolbars="toolbars"
        theme="dark"
        :placeholder="t('comment.editor.placeholder')"
        :max-length="2000"
        :footers="[]"
        :style="{ height: '25dvh' }"
      />
      <!-- 图片预览照片墙 -->
      <div v-if="uploadedImages.length || uploading" class="image-wall">
        <UploadImageWall
          :images="uploadedImages"
          :uploading="uploading"
          :uploading-count="uploadingCount"
          :max-count="MAX_COMMENT_IMAGES"
          :progress="progress"
          @remove="removeImage"
        />
      </div>
      <div class="comment-editor-footer">
        <div class="footer-left">
          <input
            ref="fileInputRef"
            type="file"
            accept="image/*"
            multiple
            style="display: none"
            @change="handleFileSelect"
          />
          <NButton
            quaternary
            size="small"
            :title="t('comment.editor.uploadImage')"
            :disabled="uploadedImages.length >= MAX_COMMENT_IMAGES || uploading"
            @click="fileInputRef.click()"
          >
            <template #icon>
              <NIcon size="18">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                  <circle cx="8.5" cy="8.5" r="1.5"></circle>
                  <polyline points="21 15 16 10 5 21"></polyline>
                </svg>
              </NIcon>
            </template>
          </NButton>
          <!-- @提及：按钮选人 + 编辑器内输入 @ 触发选人，共用同一份已选列表；
               圈子内评论 @选人按帖子所属圈子过滤可见机器人 -->
          <MentionPicker :selected-ids="selectedIds" :circle-id="circleId" @select="appendAtEnd" />
          <MentionTrigger
            :get-editor-view="getCommentEditorView"
            :selected-ids="selectedIds"
            :circle-id="circleId"
            @select="recordSelection"
          />
        </div>
        <NButton
          type="primary"
          size="medium"
          round
          :disabled="!content.trim() && !uploadedImages.length"
          :loading="submitting"
          @click="handleSubmit"
        >
          {{ t('comment.editor.submit') }}
        </NButton>
      </div>
    </div>
  </NCard>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { NCard, NButton, NIcon, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { MdEditor } from 'md-editor-v3'
import 'md-editor-v3/lib/style.css'
import { useCommentSubmit, commentSubmitErrorMessage } from '@/composables/useCommentSubmit'
import { useImageUpload } from '@/composables/useImageUpload'
import UploadImageWall from '@/components/post/detail/UploadImageWall.vue'
import MentionPicker from '@/components/post/MentionPicker.vue'
import MentionTrigger from '@/components/post/MentionTrigger.vue'
import { useMentions } from '@/composables/useMentions'
import { ensureMentionHighlight } from '@/utils/mentionHighlight'
import { auth } from '@/utils/auth'
import { filterMentionedIds } from '@/utils/mention'
import { requireLogin } from '@/utils/guest-action'
import { MessageCircle as CommentIcon } from '@vicons/tabler'

const props = defineProps({
  postId: {
    type: String,
    required: true
  },
  // 帖子所属圈子 uuid：评论 @选人按圈子作用域搜索（null = 全站搜索）
  circleId: {
    type: String,
    default: null
  },
  language: {
    type: String,
    default: 'zh-CN'
  },
  modelValue: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['update:modelValue', 'submit'])

const { t } = useI18n()
const message = useMessage()
const isLoggedIn = computed(() => auth.isAuthenticated())
const content = ref(props.modelValue)
// 提交流程（接口 + 乐观渲染对象组装）与回复 / 移动端纯文本输入共用
const { submitting, submit: submitComment } = useCommentSubmit()
const { uploading, uploadingCount, progress, uploadMany } = useImageUpload({ withProgress: true })
const uploadedImages = ref([])
const fileInputRef = ref(null)

// @提及：按钮选人与正文内输入 @ 触发选人，共用同一份已选列表与 10 人上限；
// 提交时 filterMentionedIds 过滤出正文中仍存在的 token 对应 uuid 传后端
const commentEditorRef = ref(null)
const getCommentEditorView = () => {
  const view = commentEditorRef.value?.getEditorView?.()
  if (view) ensureMentionHighlight(view)
  return view
}
const { mentionedUsers, selectedIds, recordSelection, appendAtEnd, clearMentioned } = useMentions({
  getText: () => content.value,
  setText: v => { content.value = v }
})

const MAX_COMMENT_IMAGES = 5

watch(() => props.modelValue, (val) => {
  content.value = val
})

watch(content, (val) => {
  emit('update:modelValue', val)
})

const toolbars = [
  'bold',
  'italic',
  '-',
  'quote',
  'unorderedList',
  'orderedList',
  '-',
  'codeRow',
  'link',
  'emoji',
  '-',
  'preview',
  'previewOnly'
]

const removeImage = (idx) => {
  uploadedImages.value.splice(idx, 1)
}

const handleSubmit = async () => {
  if ((!content.value.trim() && !uploadedImages.value.length) || submitting.value) return
  try {
    const extraData = uploadedImages.value.length > 0 ? { images: [...uploadedImages.value] } : null
    // 正文里被删掉的 @ 不传；须为完整 @用户名 token（@alice 不命中 @alice2）；
    // 后端对重复/@自己/不存在用户会静默过滤
    const mentionIds = filterMentionedIds(content.value, mentionedUsers.value)
    const newComment = await submitComment({
      postId: props.postId,
      content: content.value,
      extraData,
      mentionIds
    })
    message.success(t('comment.editor.success'))

    emit('submit', newComment)
    content.value = ''
    uploadedImages.value = []
    clearMentioned()
  } catch (err) {
    message.error(commentSubmitErrorMessage(err, t, 'comment.editor.failed'))
  }
}

const handleFileSelect = (e) => {
  const files = Array.from(e.target.files || [])
  if (!files.length) return
  uploadFiles(files)
  e.target.value = ''
}

const uploadFiles = async (files) => {
  if (uploadedImages.value.length >= MAX_COMMENT_IMAGES) {
    message.warning(t('upload.maxImages', { max: MAX_COMMENT_IMAGES }))
    return
  }
  const remaining = MAX_COMMENT_IMAGES - uploadedImages.value.length
  const filesToUpload = files.slice(0, remaining)
  if (filesToUpload.length < files.length) {
    message.warning(t('upload.exceedLimit', { remaining }))
  }
  try {
    const urls = await uploadMany(filesToUpload)
    uploadedImages.value = [...uploadedImages.value, ...urls]
    message.success(t('upload.success'))
  } catch (error) {
    console.error('图片上传失败:', error)
    message.error(t('upload.failed'))
  }
}
</script>

<style scoped>
.comment-editor-card {
  border-radius: 16px !important;
}

/* 访客态「登录后评论」引导：玻璃拟态按钮风格，hover 主题绿 */
.guest-comment-prompt {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 16px 20px;
  border-radius: 12px;
  border: 1px dashed rgba(255, 255, 255, 0.15);
  background: rgba(255, 255, 255, 0.02);
  color: var(--text-secondary, rgba(255, 255, 255, 0.7));
  font-size: 14px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.guest-comment-prompt:hover {
  border-color: rgba(102, 234, 194, 0.4);
  background: rgba(102, 234, 194, 0.08);
  color: var(--theme-color, #66eac2);
}

.comment-section-title {
  font-size: 18px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.9);
}

.comment-editor-header {
  margin-bottom: 12px;
}

.comment-editor-wrapper {
  border-radius: 12px;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.1);
}

.comment-editor-wrapper :deep(.md-editor) {
  border: none !important;
  border-radius: 0 !important;
}

.comment-editor-wrapper :deep(.md-editor-toolbar-wrapper) {
  border-radius: 0;
}

.comment-editor-wrapper :deep(.md-editor-content) {
  border-bottom: none !important;
}

.image-wall {
  padding: 8px 12px;
  background: rgb(24, 24, 28);
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.comment-editor-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 16px;
  background: rgb(24, 24, 28);
}

.footer-left {
  display: flex;
  align-items: center;
}

:deep(.md-editor-dark) {
  --md-bk-color: rgb(24, 24, 28);
  --md-scrollbar-bg-color: rgb(24, 24, 28);
}
</style>
