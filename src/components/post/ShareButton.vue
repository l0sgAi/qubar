<template>
  <!-- 帖子「分享」按钮：系统分享 / 复制链接 + 登录用户计数（usePostShare）。
       stat：桌面帖卡统计栏；compact：移动端紧凑列表项统计栏；button：详情页底部操作栏 -->
  <span class="share-wrap" :class="[`is-${variant}`, { 'is-shared': state.shared }]">
    <NButton
      v-if="variant === 'button'"
      class="share-action"
      size="large"
      :type="state.shared ? 'primary' : 'default'"
      :aria-label="label"
      @click="handleClick"
    >
      <template #icon>
        <NIcon size="20"><ShareIcon /></NIcon>
      </template>
      {{ state.shared ? t('share.shared') : t('share.button') }}
    </NButton>
    <button
      v-else
      type="button"
      class="share-stat"
      :aria-label="label"
      :title="t('share.button')"
      @click.stop.prevent="handleClick"
    >
      <NIcon class="share-icon" :size="variant === 'compact' ? 14 : 18"><ShareIcon /></NIcon>
      <span v-if="state.count !== null">{{ formatNumber(state.count, countCap) }}</span>
    </button>
    <!-- 首次分享成功：+1 上浮动效（key 递增保证连续触发都重播） -->
    <span v-if="bump" :key="bump" class="share-bump" aria-hidden="true" @animationend="bump = 0">+1</span>
  </span>
</template>

<script setup>
import { computed, ref } from 'vue'
import { NButton, NIcon, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Share as ShareIcon } from '@vicons/tabler'
import { sharePostLink, usePostShareState } from '@/composables/usePostShare'
import { useFormatNumber } from '@/utils/i18n'

const props = defineProps({
  postId: { type: String, required: true },
  // 系统分享面板的标题
  title: { type: String, default: '' },
  // 接口返回的 share_count；null = 该接口没给（只显示图标）
  shareCount: { type: Number, default: null },
  // 接口返回的 is_shared；只有详情 / 首页 / 热门 / 发现返回，缺省按未分享
  isShared: { type: Boolean, default: false },
  variant: {
    type: String,
    default: 'stat',
    validator: (v) => ['stat', 'compact', 'button'].includes(v)
  },
  countCap: { type: Number, default: 100_000_000 }
})

const { t } = useI18n()
const { formatNumber } = useFormatNumber()
const message = useMessage()

const state = usePostShareState(() => props.postId, () => props.shareCount, () => props.isShared)
const label = computed(() => (state.value.count === null
  ? t('share.button')
  : `${t('share.button')} ${state.value.count}`))

const bump = ref(0)

const handleClick = async () => {
  const outcome = await sharePostLink({ postId: props.postId, title: props.title })
  if (outcome.result === 'copied') message.success(t('share.linkCopied'))
  else if (outcome.result === 'failed') message.error(t('share.copyFailed'))
  if (outcome.firstShare) bump.value += 1
  if (outcome.errorKey) message.warning(t(outcome.errorKey))
}
</script>

<style scoped>
.share-wrap {
  position: relative;
  /* 抬层盖住帖卡的整卡封面链接（z-index: 1），点击只触发分享 */
  z-index: 2;
  display: inline-flex;
  align-items: center;
}

.share-stat {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin: -4px -6px;
  padding: 4px 6px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: rgba(255, 255, 255, 0.6);
  font: inherit;
  font-size: 14px;
  cursor: pointer;
  transition: color 0.2s ease, background 0.2s ease;
}

.share-icon {
  color: rgba(255, 255, 255, 0.5);
  transition: color 0.2s ease;
}

@media (hover: hover) {
  .share-stat:hover {
    color: rgba(255, 255, 255, 0.9);
    background: rgba(255, 255, 255, 0.06);
  }
}

.share-stat:focus-visible {
  outline: 2px solid rgba(102, 234, 194, 0.6);
  outline-offset: 1px;
}

.is-shared .share-stat,
.is-shared .share-icon {
  color: #66eac2;
}

/* 移动端紧凑列表项：与 .m-stat 同尺寸，触控热区靠内边距撑大 */
.is-compact .share-stat {
  gap: 4px;
  margin: -6px -8px;
  padding: 6px 8px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
}

.is-compact.is-shared .share-stat {
  color: #66eac2;
}

.share-action {
  width: 100%;
  min-width: 120px;
  border-radius: 10px;
  font-weight: 500;
}

.share-bump {
  position: absolute;
  left: 50%;
  bottom: 100%;
  font-size: 12px;
  font-weight: 600;
  color: #8af0d0;
  pointer-events: none;
  animation: share-bump 0.9s cubic-bezier(0.34, 1.56, 0.64, 1) forwards;
}

@keyframes share-bump {
  0% { opacity: 0; transform: translate(-50%, 6px) scale(0.92); }
  30% { opacity: 1; transform: translate(-50%, -2px) scale(1); }
  100% { opacity: 0; transform: translate(-50%, -14px) scale(1); }
}

@media (prefers-reduced-motion: reduce) {
  .share-bump {
    animation-duration: 0.01s;
  }
}
</style>
