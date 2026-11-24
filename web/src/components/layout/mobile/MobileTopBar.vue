<template>
  <header class="mobile-topbar">
    <!-- back：返回 + 标题 + 页面操作 -->
    <template v-if="mode === 'back'">
      <button type="button" class="icon-btn" :aria-label="t('common.back')" @click="handleBack">
        <NIcon size="22"><ChevronLeft /></NIcon>
      </button>
      <h1 class="title">{{ title }}</h1>
      <div class="actions">
        <slot name="actions" />
      </div>
    </template>

    <!-- main：logo + 搜索（访客另显登录） -->
    <template v-else>
      <SmartLink class="logo" :to="homeTarget">
        <img src="/favicon.svg" alt="logo" class="logo-icon" />
        <span class="logo-text">{{ t('common.appName') }}</span>
      </SmartLink>
      <div class="actions">
        <SmartLink class="icon-btn" to="/search" :aria-label="t('common.search')">
          <NIcon size="22"><Search /></NIcon>
        </SmartLink>
        <SmartLink v-if="!isLoggedIn" class="login-link" :to="loginTarget">
          {{ t('mobile.tab.login') }}
        </SmartLink>
      </div>
    </template>
  </header>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { ChevronLeft, Search } from '@vicons/tabler'
import SmartLink from '@/components/common/SmartLink.vue'
import { auth } from '@/utils/auth'
import { goBack, homePath, loginLocation } from '@/utils/mobileNav'

defineProps({
  // 'main' | 'back'（取自路由 meta.mobile.topBar，见 router/mobileMeta.js）
  mode: { type: String, default: 'main' },
  title: { type: String, default: '' }
})

const { t } = useI18n()
const router = useRouter()
const route = useRoute()

// 外框随页面挂载，登录跳转后重新求值，普通常量即可拿到最新登录态（同 AppHeader）
const isLoggedIn = auth.isAuthenticated()
const homeTarget = homePath(isLoggedIn)
const loginTarget = computed(() => loginLocation(route.fullPath))

const handleBack = () => goBack(router, isLoggedIn)
</script>

<style scoped>
.mobile-topbar {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  z-index: 1000;
  height: calc(var(--mobile-topbar-height) + var(--safe-top));
  padding: var(--safe-top) 8px 0;
  display: flex;
  align-items: center;
  gap: 4px;
  background: rgba(16, 16, 28, 0.88);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  border-bottom: 1px solid var(--glass-border);
}

.logo {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-left: 4px;
  text-decoration: none;
  flex: 1;
  min-width: 0;
}

.logo-icon {
  width: 28px;
  height: 28px;
  flex-shrink: 0;
}

.logo-text {
  font-size: 1.2rem;
  font-weight: 700;
  color: #66eac2;
}

.title {
  flex: 1;
  min-width: 0;
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  text-align: center;
}

.actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  /* back 模式下与左侧返回按钮等宽，保证标题居中 */
  min-width: 44px;
}

.icon-btn {
  width: 44px;
  height: 44px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: none;
  border-radius: 12px;
  background: transparent;
  color: var(--text-primary);
  text-decoration: none;
  cursor: pointer;
  -webkit-tap-highlight-color: transparent;
  touch-action: manipulation;
}

.icon-btn:active {
  background: rgba(255, 255, 255, 0.08);
}

.login-link {
  height: 32px;
  padding: 0 14px;
  display: inline-flex;
  align-items: center;
  border-radius: 16px;
  background: var(--primary-gradient);
  color: #0a0e27;
  font-size: 14px;
  font-weight: 600;
  text-decoration: none;
}
</style>
