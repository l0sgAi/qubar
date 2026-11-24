<template>
  <!-- 作者本人看到的非已发布状态提示条（审核中 / 审核较慢 / 驳回 / 屏蔽等待复核） -->
  <NAlert
    v-if="variant"
    class="post-status-banner"
    :class="`is-${variant}`"
    :type="variant === 'rejected' ? 'warning' : 'info'"
    :show-icon="true"
  >
    <div class="banner-body">
      <span class="banner-text">{{ text }}</span>
      <NButton
        v-if="variant === 'reviewing' || variant === 'slow'"
        class="banner-refresh"
        size="small"
        secondary
        round
        :loading="refreshing"
        @click="$emit('refresh')"
      >
        {{ t('post.reviewing.refresh') }}
      </NButton>
    </div>
  </NAlert>
</template>

<script setup>
import { computed } from 'vue'
import { NAlert, NButton } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { POST_STATUS } from '@/composables/usePostReview'
import { usePostStatus } from '@/composables/usePostStatus'

const props = defineProps({
  status: {
    type: Number,
    default: POST_STATUS.PUBLISHED
  },
  // 自动轮询已用完仍在审核中（hold 模式）
  slow: {
    type: Boolean,
    default: false
  },
  refreshing: {
    type: Boolean,
    default: false
  }
})

defineEmits(['refresh'])

const { t } = useI18n()
const { getPostStatusInfo } = usePostStatus()

const variant = computed(() => {
  if (props.status === POST_STATUS.REVIEWING) return props.slow ? 'slow' : 'reviewing'
  if (props.status === POST_STATUS.REJECTED) return 'rejected'
  if (props.status === POST_STATUS.BLOCKED) return 'blocked'
  return null
})

const text = computed(() => ({
  reviewing: t('post.reviewing.banner'),
  slow: t('post.reviewing.slow'),
  // 驳回 / 屏蔽与「我的帖子」状态标签的说明共用一份文案
  rejected: getPostStatusInfo(POST_STATUS.REJECTED).hint,
  blocked: getPostStatusInfo(POST_STATUS.BLOCKED).hint
})[variant.value])
</script>

<style scoped>
/* 绿色玻璃风提示条（NAlert 非 teleport 组件，scoped :deep 可命中），与 CircleAgents 的 .reply-note 一致 */
.post-status-banner {
  margin: 0 0 16px;
  border-radius: 12px !important;
  background: rgba(102, 234, 194, 0.06) !important;
  border: 1px solid rgba(102, 234, 194, 0.18) !important;
}

.post-status-banner.is-rejected {
  background: rgba(242, 201, 125, 0.06) !important;
  border-color: rgba(242, 201, 125, 0.22) !important;
}

.banner-body {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px 12px;
}

.banner-text {
  flex: 1 1 200px;
  min-width: 0;
  color: var(--text-secondary);
  word-break: break-word;
}

.banner-refresh {
  flex-shrink: 0;
}
</style>
