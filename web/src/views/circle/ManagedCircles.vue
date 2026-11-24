<template>
  <div class="managed-circles-page">
    <!-- 顶栏 -->
    <AppHeader />

    <!-- 侧边栏 -->
    <SideNav @collapsed="offset = 64" @expanded="offset = 260" />

    <!-- 主内容区域 -->
    <div class="content-wrapper" :style="{ 'margin-left': `${offset}px`, width: `calc(100% - ${offset}px)` }">
      <div class="main-content">
        <div class="managed-container">
          <div class="managed-header">
            <div class="header-left">
              <h1 class="page-title">{{ t('circle.managed.title') }}</h1>
              <p class="page-sub">{{ t('circle.managed.subtitle') }}</p>
            </div>
            <NInput
              v-model:value="keyword"
              size="small"
              round
              clearable
              :placeholder="t('agent.circleList.searchPlaceholder')"
              class="search-input"
            >
              <template #prefix>
                <NIcon size="14"><SearchIcon /></NIcon>
              </template>
            </NInput>
          </div>

          <!-- 选择圈子进入其「内容审核」页（GET /circle/manage/list） -->
          <ManagedCircleList v-model:keyword="keyword" target="moderation" />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
// 我管理的圈子：举报通知（类型 9）找不到帖子所属圈子时的兜底落点，选圈子进入其内容审核页
import { ref } from 'vue'
import { NInput, NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Search as SearchIcon } from '@vicons/tabler'
import AppHeader from '@/components/layout/AppHeader.vue'
import SideNav from '@/components/layout/SideNav.vue'
import ManagedCircleList from '@/components/circle/ManagedCircleList.vue'

const { t } = useI18n()
const offset = ref(260)
const keyword = ref('')
</script>

<style scoped>
.managed-circles-page {
  min-height: 100vh;
}

.content-wrapper {
  display: flex;
  padding-top: var(--header-height);
}

.main-content {
  flex: 1;
  min-width: 0;
  padding: 24px;
}

/* 主容器：与机器人管理页同款暗色玻璃卡片 */
.managed-container {
  max-width: 1100px;
  margin: 0 auto;
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid var(--glass-border);
  border-radius: 24px;
  box-shadow: var(--shadow-lg);
  padding: 28px;
}

.managed-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

.page-title {
  font-size: 1.4rem;
  font-weight: 700;
  margin: 0;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.page-sub {
  margin: 4px 0 0;
  font-size: 0.85rem;
  color: rgba(255, 255, 255, 0.5);
}

.search-input {
  width: 220px;
}
</style>
