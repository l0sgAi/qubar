<template>
  <div class="reply-editor">
    <MdEditor
      ref="replyEditorRef"
      v-model="content"
      :language="language"
      :preview="false"
      :toolbars="toolbars"
      theme="dark"
      :placeholder="replyToName ? `@${t('comment.reply.placeholder', { name: replyToName })}` : t('comment.editor.placeholder')"
      :max-length="2000"
      :footers="[]"
      :style="{ height: '17dvh' }"
    />
    <!-- 图片预览照片墙 -->
    <div v-if="uploadedImages.length || uploading" class="image-wall">
      <UploadImageWall
        :images="uploadedImages"
        :uploading="uploading"
        :uploading-count="uploadingCount"
        :max-count="MAX_COMMENT_IMAGES"
        :thumb-size="64"
        :gap="6"
        :progress="progress"
        @remove="removeImage"
      />
    </div>
    <div class="reply-editor-footer">
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
            <NIcon size="16">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
                <circle cx="8.5" cy="8.5" r="1.5"></circle>
                <polyline points="21 15 16 10 5 21"></polyline>
              </svg>
            </NIcon>
          </template>
        </NButton>
        <!-- @提及：按钮选人 + 编辑器内输入 @ 触发选人，共用同一份已选列表 -->
        <MentionPicker :selected-ids="selectedIds" :circle-id="circleId" :icon-size="16" @select="appendAtEnd" />
        <MentionTrigger
          :get-editor-view="getReplyEditorView"
          :selected-ids="selectedIds"
          :circle-id="circleId"
          @select="recordSelection"
        />
      </div>
      <div class="footer-right">
        <NButton size="small" quaternary @click="$emit('cancel')">{{ t('common.cancel') }}</NButton>
        <NButton
          type="primary"
          size="small"
          round
          :disabled="!content.trim() && !uploadedImages.length"
          :loading="submitting"
          @click="handleSubmit"
        >
          {{ t('comment.actions.reply') }}
        </NButton>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { NButton, NIcon, useMessage } from 'naive-ui'
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

const props = defineProps({
  postId: {
    type: String,
    required: true
  },
  // 帖子所属圈子 uuid：回复 @选人按圈子作用域搜索（null = 全站搜索）
  circleId: {
    type: String,
    default: null
  },
  rootId: {
    type: String,
    default: null
  },
  // 直接回复根评论时为 null（此时用 rootId 作为 reply_to_id）
  replyToId: {
    type: String,
    default: null
  },
  replyToName: {
    type: String,
    default: ''
  },
  language: {
    type: String,
    default: 'zh-CN'
  }
})

const emit = defineEmits(['submit', 'cancel'])

const { t } = useI18n()
const message = useMessage()
const content = ref('')
// 提交流程（接口 + 乐观渲染对象组装）与顶层评论 / 移动端纯文本输入共用
const { submitting, submit: submitComment } = useCommentSubmit()
const { uploading, uploadingCount, progress, uploadMany } = useImageUpload({ withProgress: true })
const uploadedImages = ref([])
const fileInputRef = ref(null)

// @提及：按钮选人与正文内输入 @ 触发选人，共用同一份已选列表与 10 人上限；
// 提交时 filterMentionedIds 过滤出正文中仍存在的 token 对应 uuid 传后端
const replyEditorRef = ref(null)
const getReplyEditorView = () => {
  const view = replyEditorRef.value?.getEditorView?.()
  if (view) ensureMentionHighlight(view)
  return view
}
const { mentionedUsers, selectedIds, recordSelection, appendAtEnd, clearMentioned } = useMentions({
  getText: () => content.value,
  setText: v => { content.value = v }
})

const MAX_COMMENT_IMAGES = 5

const toolbars = [
  'bold',
  'italic',
  '_',
  '-',
  'preview'
]

const removeImage = (idx) => {
  uploadedImages.value.splice(idx, 1)
}

const handleSubmit = async () => {
  // 访客发回复前置拦截：弹登录引导
  if (!auth.isAuthenticated()) {
    requireLogin('comment')
    return
  }
  if ((!content.value.trim() && !uploadedImages.value.length) || submitting.value) return
  try {
    const extraData = uploadedImages.value.length > 0 ? { images: [...uploadedImages.value] } : null
    // 正文里被删掉的 @ 不传；须为完整 @用户名 token（@alice 不命中 @alice2）；
    // 后端对重复/@自己/不存在用户会静默过滤
    const mentionIds = filterMentionedIds(content.value, mentionedUsers.value)
    const newReply = await submitComment({
      postId: props.postId,
      content: content.value,
      reply: { rootId: props.rootId, replyToId: props.replyToId, replyToName: props.replyToName },
      extraData,
      mentionIds
    })
    message.success(t('comment.reply.success'))

    content.value = ''
    uploadedImages.value = []
    clearMentioned()
    emit('submit', newReply)
  } catch (err) {
    message.error(commentSubmitErrorMessage(err, t, 'comment.reply.failed'))
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
.reply-editor {
  margin-top: 10px;
  border-radius: 10px;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.reply-editor :deep(.md-editor) {
  border: none !important;
  border-radius: 0 !important;
}

.reply-editor :deep(.md-editor-toolbar-wrapper) {
  border-radius: 0;
}

.reply-editor :deep(.md-editor-content) {
  border-bottom: none !important;
}

.image-wall {
  padding: 6px 10px;
  background: rgb(24, 24, 28);
  border-top: 1px solid rgba(255, 255, 255, 0.06);
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.reply-editor-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 10px;
  background: rgb(24, 24, 28);
}

.footer-left {
  display: flex;
  align-items: center;
}

.footer-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

:deep(.md-editor-dark) {
  --md-bk-color: rgb(24, 24, 28);
  --md-scrollbar-bg-color: rgb(24, 24, 28);
}
</style>
