<template>
  <!-- 我的互关占位页（路由 follow-mutual），工作流 B 替换 -->
  <div class="placeholder-page">
    <AppShell v-model:offset="offset" />
    <div class="main-content" :style="contentStyle">
      <div class="placeholder-wrap">
        <h2 v-if="!mobileShell" class="page-title">{{ t('title.mutual') }}</h2>
        <p class="page-desc">{{ t('common.featureInDevelopment') }}</p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import AppShell from '@/components/layout/AppShell.vue'
import { useAppShell } from '@/composables/useAppShell'

const { t } = useI18n()
const { offset, mobileShell, contentStyle } = useAppShell()
</script>

<style scoped>
.placeholder-page {
  min-height: 100vh;
  position: relative;
}

.main-content {
  margin-top: var(--header-height);
  min-height: calc(100vh - var(--header-height));
  padding: 24px;
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.placeholder-wrap {
  max-width: 760px;
  margin: 0 auto;
}

.page-title {
  margin: 12px 0 6px;
  font-size: 1.4rem;
  font-weight: 700;
  color: var(--text-primary);
}

.page-desc {
  margin: 0;
  font-size: 0.88rem;
  color: var(--text-secondary);
}
</style>
