<template>
  <!-- 举报帖子：桌面居中弹窗，移动端外框下改为底部抽屉（与评论输入抽屉同一套玻璃样式，全局样式见 main.css .report-sheet）。
       两种容器共用同一块面板，键盘弹出时抽屉由 useKeyboardInset 抬到键盘上方并收窄最大高度，不被顶出视口 -->
  <component
    :is="mobileShell ? NDrawer : NModal"
    v-bind="containerProps"
    :show="show"
    @update:show="handleUpdateShow"
  >
    <div class="report-panel" :class="{ 'is-sheet': mobileShell }">
      <div class="report-header">
        <span class="report-title">{{ t('post.report.title') }}</span>
        <button type="button" class="report-close" :aria-label="t('common.cancel')" :disabled="submitting" @click="close">
          <NIcon size="20"><CloseIcon /></NIcon>
        </button>
      </div>

      <div class="report-body">
        <div class="report-label">{{ t('post.report.reasonLabel') }}</div>
        <NRadioGroup v-model:value="reasonType" name="report-reason" class="report-reasons">
          <NRadio
            v-for="r in REPORT_REASON_TYPES"
            :key="r.value"
            :value="r.value"
            class="report-reason"
          >
            {{ t(`report.reasons.${r.key}`) }}
          </NRadio>
        </NRadioGroup>

        <p v-if="reasonType === SELF_HARM" class="report-care" role="note">
          {{ t('post.report.selfHarmHint') }}
        </p>

        <NInput
          v-model:value="reason"
          type="textarea"
          class="report-detail"
          :maxlength="REPORT_REASON_MAX"
          show-count
          :autosize="{ minRows: 2, maxRows: 4 }"
          :placeholder="t('post.report.detailPlaceholder')"
        />

        <p class="report-note">{{ t('post.report.anonymousNote') }}</p>
      </div>

      <div class="report-footer">
        <NButton class="report-cancel" :disabled="submitting" @click="close">{{ t('common.cancel') }}</NButton>
        <NButton
          class="report-submit"
          type="primary"
          :disabled="!reasonType"
          :loading="submitting"
          @click="submit"
        >
          {{ t('post.report.submit') }}
        </NButton>
      </div>
    </div>
  </component>
</template>

<script setup>
import { ref, computed, watch } from 'vue'
import { NModal, NDrawer, NRadioGroup, NRadio, NInput, NButton, NIcon, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { X as CloseIcon } from '@vicons/tabler'
import { reportPost } from '@/api/post'
import { REPORT_REASON_TYPES, REPORT_REASON_MAX } from '@/constants/report'
import { markPostReported, reportErrorKey } from '@/composables/usePostReport'
import { useMobileShell } from '@/composables/useAppShell'
import { useKeyboardInset } from '@/composables/useKeyboardInset'

const props = defineProps({
  show: { type: Boolean, default: false },
  postId: { type: String, required: true }
})

const emit = defineEmits(['update:show', 'reported'])

// 自伤轻生：额外显示关怀提示
const SELF_HARM = 7

const { t } = useI18n()
const message = useMessage()
const mobileShell = useMobileShell()

const reasonType = ref(null)
const reason = ref('')
const submitting = ref(false)

const { inset } = useKeyboardInset(() => props.show && mobileShell.value)

const containerProps = computed(() => mobileShell.value
  ? {
      placement: 'bottom',
      height: 'auto',
      class: 'report-sheet',
      autoFocus: false,
      maskClosable: !submitting.value,
      // 键盘弹出：抬到键盘上方，最大高度同步收窄，面板内部滚动
      style: { bottom: `${inset.value}px`, maxHeight: `calc(100% - ${inset.value}px - 24px)` }
    }
  : {
      class: 'report-post-modal',
      autoFocus: false,
      maskClosable: !submitting.value
    })

// 每次打开都从空表单开始
watch(() => props.show, (show) => {
  if (!show) return
  reasonType.value = null
  reason.value = ''
})

const close = () => {
  if (submitting.value) return
  emit('update:show', false)
}

const handleUpdateShow = (show) => {
  if (!show) close()
}

// 成功 / 重复举报 / 帖子已不可举报：关闭弹窗；频繁 / 其他失败：保留内容方便稍后重试
const submit = async () => {
  if (!reasonType.value || submitting.value) return
  const payload = { post_id: props.postId, reason_type: reasonType.value }
  const detail = reason.value.trim()
  if (detail) payload.reason = detail

  submitting.value = true
  let done = false
  try {
    await reportPost(payload)
    markPostReported(props.postId)
    emit('reported', props.postId)
    message.success(t('post.report.success'))
    done = true
  } catch (error) {
    const key = reportErrorKey(error)
    if (key === 'already') {
      markPostReported(props.postId)
      emit('reported', props.postId)
      message.warning(t('post.report.already'))
      done = true
    } else if (key === 'unavailable') {
      message.warning(t('post.report.unavailable'))
      done = true
    } else if (key === 'tooOften') {
      message.warning(t('post.report.tooOften'))
    } else if (key === 'invalid') {
      message.error(t('post.report.invalid'))
    } else {
      message.error(t('post.report.failed'))
    }
  } finally {
    submitting.value = false
  }
  if (done) emit('update:show', false)
}
</script>

<style scoped>
.report-panel {
  display: flex;
  flex-direction: column;
  width: 480px;
  max-width: 92vw;
  max-height: 86vh;
  background: rgba(22, 22, 38, 0.92);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 16px;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.5);
  color: var(--text-primary);
}

/* 抽屉：容器（.n-drawer.report-sheet）已有玻璃背景与圆角，面板铺满并在内部滚动 */
.report-panel.is-sheet {
  width: 100%;
  max-width: none;
  max-height: none;
  flex: 1;
  min-height: 0;
  background: transparent;
  backdrop-filter: none;
  -webkit-backdrop-filter: none;
  border: none;
  border-radius: 0;
  box-shadow: none;
}

.report-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  padding: 14px 16px 12px 20px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
}

.report-title {
  font-size: 16px;
  font-weight: 600;
}

.report-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  margin-right: -6px;
  padding: 0;
  border: none;
  border-radius: 10px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}

@media (hover: hover) {
  .report-close:hover {
    background: rgba(255, 255, 255, 0.08);
    color: var(--text-primary);
  }
}

.report-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  overscroll-behavior: contain;
  padding: 16px 20px 4px;
}

.report-label {
  margin-bottom: 10px;
  font-size: 13px;
  color: var(--text-secondary);
}

/* 理由：两列，整行可点（label 铺满），移动端行高 ≥ 44px */
.report-reasons {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px;
  width: 100%;
}

.report-reasons :deep(.n-radio) {
  display: flex;
  align-items: center;
  min-height: 40px;
  /* 英文理由在窄屏会折成两行 */
  padding: 6px 12px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 10px;
  background: rgba(255, 255, 255, 0.03);
  transition: background 0.2s, border-color 0.2s;
}

.report-reasons :deep(.n-radio.n-radio--checked) {
  border-color: rgba(102, 234, 194, 0.5);
  background: rgba(102, 234, 194, 0.1);
}

.report-reasons :deep(.n-radio__label) {
  font-size: 14px;
}

.is-sheet .report-reasons :deep(.n-radio) {
  min-height: 44px;
}

.report-care {
  margin: 12px 0 0;
  padding: 10px 12px;
  border-radius: 10px;
  font-size: 13px;
  line-height: 1.6;
  color: #8af0d0;
  background: rgba(102, 234, 194, 0.08);
  border: 1px solid rgba(102, 234, 194, 0.2);
}

.report-detail {
  margin-top: 14px;
}

.report-note {
  margin: 10px 0 0;
  font-size: 12px;
  color: var(--text-tertiary);
}

.report-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  flex-shrink: 0;
  padding: 14px 20px 16px;
}

.is-sheet .report-footer {
  padding-bottom: calc(12px + var(--safe-bottom, 0px));
}

.is-sheet .report-footer .n-button {
  flex: 1;
  height: 44px;
}

.report-footer .n-button {
  border-radius: 10px;
}
</style>
