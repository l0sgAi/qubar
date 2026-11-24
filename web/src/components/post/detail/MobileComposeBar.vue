<template>
  <div class="compose-bar" :class="{ 'is-fixed': fixed }">
    <!-- 移动端「说点什么…」伪输入条：点击打开评论输入抽屉（MobileCommentComposer）。
         fixed：帖子详情页底部常驻；否则内嵌（回复抽屉底部）。
         传 count 时右侧显示评论数，点击 emit count-click -->
    <button type="button" class="compose-bar-trigger" @click="emit('compose')">
      <NIcon size="16" class="compose-bar-icon"><EditIcon /></NIcon>
      <span class="compose-bar-placeholder">{{ placeholder }}</span>
    </button>
    <button
      v-if="count !== null"
      type="button"
      class="compose-bar-count"
      :aria-label="t('mobile.comment.toComments')"
      @click="emit('count-click')"
    >
      <NIcon size="20"><CommentIcon /></NIcon>
      <span>{{ count }}</span>
    </button>
  </div>
</template>

<script setup>
import { NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { MessageCircle as CommentIcon, Pencil as EditIcon } from '@vicons/tabler'

defineProps({
  placeholder: {
    type: String,
    required: true
  },
  count: {
    type: Number,
    default: null
  },
  fixed: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['compose', 'count-click'])

const { t } = useI18n()
</script>

<style scoped>
.compose-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  width: 100%;
  height: var(--compose-bar-height);
}

/* 详情页底部常驻：与 TabBar 同一套底栏玻璃样式，底部让出 Home Indicator 安全区 */
.compose-bar.is-fixed {
  position: fixed;
  left: 0;
  right: 0;
  bottom: 0;
  z-index: 1000;
  height: calc(var(--compose-bar-height) + var(--safe-bottom));
  padding: 0 12px var(--safe-bottom);
  background: rgba(16, 16, 28, 0.92);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border-top: 1px solid var(--glass-border);
}

.compose-bar-trigger {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 14px;
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 18px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--text-tertiary);
  font-size: 14px;
  text-align: left;
  cursor: text;
  transition: border-color 0.2s, background 0.2s;
}

.compose-bar-trigger:active {
  border-color: rgba(102, 234, 194, 0.4);
  background: rgba(102, 234, 194, 0.08);
}

.compose-bar-icon {
  flex-shrink: 0;
}

.compose-bar-placeholder {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.compose-bar-count {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 4px;
  min-width: 44px;
  height: 44px;
  padding: 0 4px;
  border: none;
  background: transparent;
  color: var(--text-secondary);
  font-size: 14px;
  font-variant-numeric: tabular-nums;
  cursor: pointer;
}

.compose-bar-count:active {
  color: #66eac2;
}
</style>
