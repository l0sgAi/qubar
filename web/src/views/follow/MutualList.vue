<!-- 我的互关（路由 follow-mutual，需登录；GET /follow/mutual 只能看自己的，不受隐私开关影响） -->
<template>
  <div class="mutual-list-page">
    <AppShell v-model:offset="offset" />
    <div class="content-wrapper" :style="contentStyle">
      <div class="main-content">
        <div class="list-container">
          <h1 v-if="!mobileShell" class="page-title">{{ t('title.mutual') }}</h1>
          <FollowUserList
            list-key="mutual"
            :fetch-page="getMutualList"
            :empty-text="t('follow.empty.mutual')" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { useI18n } from 'vue-i18n'
import AppShell from '@/components/layout/AppShell.vue'
import FollowUserList from '@/components/follow/FollowUserList.vue'
import { useAppShell } from '@/composables/useAppShell'
import { getMutualList } from '@/api/follow'

const { t } = useI18n()
const { offset, mobileShell, contentStyle } = useAppShell()
</script>

<style scoped>
.mutual-list-page {
  min-height: 100vh;
}

.content-wrapper {
  padding-top: var(--header-height);
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.main-content {
  display: flex;
  justify-content: center;
  padding: 24px;
}

.list-container {
  width: 100%;
  max-width: 720px;
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  box-shadow: var(--shadow-md);
  padding: 20px 24px;
}

.page-title {
  margin: 0 0 12px;
  font-size: 20px;
  font-weight: 600;
  color: var(--text-primary);
}

@media (max-width: 768px) {
  .main-content {
    padding: 0;
  }

  .list-container {
    max-width: none;
    border: none;
    border-radius: 0;
    box-shadow: none;
    padding: 8px 4px 16px;
  }
}
</style>
