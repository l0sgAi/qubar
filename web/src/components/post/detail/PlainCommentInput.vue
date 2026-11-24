<template>
  <!-- 移动端纯文本评论 / 回复的输入区（见 docs/mobile-adaptation-plan.md 6.6），放在 MobileCommentComposer 抽屉里。
       传 rootId 即回复模式；内容由父组件 v-model:value 持有（按回复目标保留草稿）。
       不带 Markdown 工具栏、配图、@ 提及；内容仍按 Markdown 渲染，MdPreview 开启 breaks，单换行即换行 -->
  <div class="plain-comment">
    <div class="input-box">
      <NInput
        ref="inputRef"
        :value="value"
        type="textarea"
        :autosize="{ minRows: 1, maxRows: 6 }"
        :maxlength="MAX_LENGTH"
        :placeholder="placeholder"
        class="plain-textarea"
        @update:value="(v) => emit('update:value', v)"
      />
      <NButton
        type="primary"
        size="small"
        round
        class="send-btn"
        :disabled="!value.trim()"
        :loading="submitting"
        @click="handleSubmit"
      >
        <template #icon><NIcon><SendIcon /></NIcon></template>
        {{ t('mobile.comment.send') }}
      </NButton>
    </div>
    <div class="input-footer">
      <span class="rich-hint">{{ t('mobile.comment.richHint') }}</span>
      <span v-if="value.length >= COUNT_THRESHOLD" class="char-count" :class="{ 'is-full': value.length >= MAX_LENGTH }">
        {{ value.length }}/{{ MAX_LENGTH }}
      </span>
    </div>
  </div>
</template>

<script setup>
import { ref, computed } from 'vue'
import { NInput, NButton, NIcon, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Send as SendIcon } from '@vicons/tabler'
import { useCommentSubmit, commentSubmitErrorMessage } from '@/composables/useCommentSubmit'

// 与桌面 MdEditor 的 max-length 一致；接近上限时才显示字数
const MAX_LENGTH = 2000
const COUNT_THRESHOLD = 1800

const props = defineProps({
  postId: {
    type: String,
    required: true
  },
  value: {
    type: String,
    default: ''
  },
  // 回复模式：根评论 id（为 null 表示发表顶层评论）
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
  }
})

const emit = defineEmits(['update:value', 'submit'])

const { t } = useI18n()
const message = useMessage()
const { submitting, submit } = useCommentSubmit()

const isReply = computed(() => props.rootId !== null)
const inputRef = ref(null)

const placeholder = computed(() => (isReply.value && props.replyToName
  ? t('mobile.comment.replyPlaceholder', { name: props.replyToName })
  : t('mobile.comment.placeholder')))

// 成功：提示、清空草稿、emit submit（收起抽屉由父组件处理）；失败：提示错误，保留内容
const handleSubmit = async () => {
  if (!props.value.trim() || submitting.value) return
  try {
    const created = await submit({
      postId: props.postId,
      content: props.value,
      reply: isReply.value
        ? { rootId: props.rootId, replyToId: props.replyToId, replyToName: props.replyToName }
        : null
    })
    message.success(t(isReply.value ? 'comment.reply.success' : 'comment.editor.success'))
    emit('update:value', '')
    emit('submit', created)
  } catch (err) {
    message.error(commentSubmitErrorMessage(err, t, isReply.value ? 'comment.reply.failed' : 'comment.editor.failed'))
  }
}

defineExpose({
  focus: () => inputRef.value?.focus(),
  blur: () => inputRef.value?.blur()
})
</script>

<style scoped>
.input-box {
  display: flex;
  align-items: flex-end;
  gap: 8px;
}

.plain-textarea {
  flex: 1;
  /* ≥ 16px：iOS 聚焦输入框时不自动缩放页面 */
  --n-font-size: 16px !important;
  font-size: 16px;
}

.send-btn {
  flex-shrink: 0;
  height: 34px;
}

.input-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-top: 8px;
  min-height: 20px;
}

.rich-hint {
  font-size: 12px;
  color: var(--text-tertiary);
}

.char-count {
  flex-shrink: 0;
  font-size: 12px;
  font-variant-numeric: tabular-nums;
  color: var(--text-secondary);
}

.char-count.is-full {
  color: #f87171;
}
</style>
