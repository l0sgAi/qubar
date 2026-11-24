<template>
  <!-- 帖子状态标签：有说明时带提示图标，桌面悬停、触屏点按显示说明（不依赖 hover）。
       用在 MobilePostItem 的 #meta 插槽 / 桌面卡片头部，两处都已抬层盖住封面链接，点按不会跳转 -->
  <NPopover
    v-if="info.hint"
    :trigger="tapToShow ? 'click' : 'hover'"
    placement="top"
    :style="{ maxWidth: '260px' }"
  >
    <template #trigger>
      <button
        type="button"
        class="post-status-tag"
        :aria-label="`${info.text}: ${info.hint}`"
      >
        <NTag :size="size" round :type="info.type">
          {{ info.text }}
          <template #icon>
            <NIcon class="post-status-hint-icon"><InfoIcon /></NIcon>
          </template>
        </NTag>
      </button>
    </template>
    <span class="post-status-hint">{{ info.hint }}</span>
  </NPopover>
  <NTag v-else :size="size" round :type="info.type">{{ info.text }}</NTag>
</template>

<script setup>
import { computed } from 'vue'
import { NPopover, NTag, NIcon } from 'naive-ui'
import { InfoCircle as InfoIcon } from '@vicons/tabler'
import { usePostStatus } from '@/composables/usePostStatus'
import { useMobileShell } from '@/composables/useAppShell'

const props = defineProps({
  status: { type: Number, required: true },
  size: { type: String, default: 'tiny' }
})

const { getPostStatusInfo } = usePostStatus()
const info = computed(() => getPostStatusInfo(props.status))

// 移动端外框或无 hover 能力的触屏设备：改为点按显示
const mobileShell = useMobileShell()
const noHover = typeof window !== 'undefined' && !!window.matchMedia?.('(hover: none)').matches
const tapToShow = computed(() => mobileShell.value || noHover)
</script>

<style scoped>
/* 重置按钮外观，只作为可聚焦 / 可点按的触发器 */
.post-status-tag {
  display: inline-flex;
  padding: 0;
  border: 0;
  background: none;
  font: inherit;
  color: inherit;
  cursor: help;
}

.post-status-tag:focus-visible {
  outline: 2px solid rgba(102, 234, 194, 0.6);
  outline-offset: 2px;
  border-radius: 999px;
}

.post-status-hint-icon {
  opacity: 0.85;
}

.post-status-hint {
  font-size: 13px;
  line-height: 1.5;
}
</style>
