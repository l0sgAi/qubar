<template>
  <!-- 移动端「暂不支持页」：由 App.vue 在移动端外框下替换 meta.mobile.supported=false 的页面，URL 不变 -->
  <div class="mobile-unsupported-page">
    <AppShell />

    <div class="main-content" :style="contentStyle">
      <div class="card">
        <div class="icon">
          <NIcon size="40"><DeviceDesktop /></NIcon>
        </div>
        <h2 class="title">{{ t('mobile.unsupported.title') }}</h2>
        <p class="desc">{{ t('mobile.unsupported.desc') }}</p>

        <NButton type="primary" block size="large" class="action" @click="handleCopy">
          <template #icon><NIcon><Copy /></NIcon></template>
          {{ t('mobile.unsupported.copyLink') }}
        </NButton>
        <a
          v-if="APP_DOWNLOAD_URL"
          class="download"
          :href="APP_DOWNLOAD_URL"
          target="_blank"
          rel="noopener"
        >
          {{ t('mobile.unsupported.downloadApp') }}
        </a>
        <div class="row">
          <NButton secondary block size="large" @click="handleBack">{{ t('common.back') }}</NButton>
          <NButton secondary block size="large" @click="handleHome">{{ t('mobile.unsupported.backHome') }}</NButton>
        </div>

        <p class="hint">{{ t('mobile.unsupported.desktopHint') }}</p>
      </div>
    </div>
  </div>
</template>

<script setup>
import { useRouter } from 'vue-router'
import { NButton, NIcon, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { DeviceDesktop, Copy } from '@vicons/tabler'
import AppShell from '@/components/layout/AppShell.vue'
import { useAppShell } from '@/composables/useAppShell'
import { auth } from '@/utils/auth'
import { copyText } from '@/utils/share'
import { goBack, homePath } from '@/utils/mobileNav'
import { APP_DOWNLOAD_URL } from '@/config'

const { t } = useI18n()
const router = useRouter()
const message = useMessage()
const { contentStyle } = useAppShell()

const handleCopy = async () => {
  if (await copyText(window.location.href)) {
    message.success(t('mobile.linkCopied'))
  } else {
    message.error(t('mobile.copyFailed'))
  }
}

const handleBack = () => goBack(router, auth.isAuthenticated())
const handleHome = () => router.replace(homePath(auth.isAuthenticated()))
</script>

<style scoped>
.mobile-unsupported-page {
  min-height: 100vh;
  min-height: 100dvh;
}

.main-content {
  padding: calc(var(--header-height) + 24px) 16px 0;
  display: flex;
  justify-content: center;
}

.card {
  width: 100%;
  max-width: 420px;
  padding: 32px 20px 24px;
  border-radius: 20px;
  background: var(--glass-bg);
  border: 1px solid var(--glass-border);
  text-align: center;
}

.icon {
  width: 72px;
  height: 72px;
  margin: 0 auto 16px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #66eac2;
  background: rgba(102, 234, 194, 0.12);
}

.title {
  font-size: 18px;
  font-weight: 600;
  color: var(--text-primary);
  margin-bottom: 8px;
}

.desc {
  font-size: 14px;
  line-height: 1.6;
  color: var(--text-secondary);
  margin-bottom: 24px;
}

.action {
  margin-bottom: 12px;
}

.download {
  display: block;
  margin-bottom: 12px;
  font-size: 15px;
  color: #66eac2;
}

.row {
  display: flex;
  gap: 12px;
}

.hint {
  margin-top: 20px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--text-tertiary);
}
</style>
