<template>
  <!-- 移动端「发现 | 热榜」分段切换：TabBar 只有「发现」一项，热榜入口放在这里（见 docs/mobile-adaptation-plan.md 6.2） -->
  <nav class="feed-segment">
    <SmartLink
      v-for="item in ITEMS"
      :key="item.path"
      class="segment-item"
      :class="{ active: route.path === item.path }"
      :to="item.path"
      replace
    >
      {{ t(item.label) }}
    </SmartLink>
  </nav>
</template>

<script setup>
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import SmartLink from '@/components/common/SmartLink.vue'

const ITEMS = [
  { path: '/discover', label: 'mobile.segment.discover' },
  { path: '/hot', label: 'mobile.segment.hot' }
]

const route = useRoute()
const { t } = useI18n()
</script>

<style scoped>
.feed-segment {
  display: flex;
  gap: 4px;
  padding: 4px;
  margin-bottom: 12px;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--glass-border);
}

.segment-item {
  flex: 1;
  height: 36px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 9px;
  color: var(--text-secondary);
  font-size: 14px;
  font-weight: 500;
  text-decoration: none;
  transition: background 0.2s, color 0.2s;
}

.segment-item.active {
  background: rgba(102, 234, 194, 0.16);
  color: #66eac2;
}
</style>
