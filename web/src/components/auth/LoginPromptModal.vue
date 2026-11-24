<template>
  <NModal
    :show="showPrompt"
    @update:show="handleShowChange"
    preset="card"
    class="login-prompt-modal"
    :style="{ width: '420px', maxWidth: '90vw', borderRadius: '24px' }"
    :bordered="false"
    :closable="false"
    size="huge"
    :mask-closable="true"
    content-style="padding: 28px 28px 24px;"
  >
    <!-- 顶部图标 -->
    <div class="prompt-icon-area">
      <div class="prompt-icon-wrap">
        <NIcon :component="LoginIcon" :size="32" />
      </div>
    </div>

    <!-- 标题 + 引导文案 -->
    <h3 class="prompt-title">{{ t('login.guestPrompt.title') }}</h3>
    <p class="prompt-desc">
      {{ actionText }}
    </p>

    <!-- 操作按钮 -->
    <div class="prompt-actions">
      <NButton
        size="large"
        block
        class="prompt-primary-btn"
        @click="goLogin"
      >
        {{ t('login.guestPrompt.goLogin') }}
      </NButton>
      <NButton
        size="large"
        block
        tertiary
        class="prompt-secondary-btn"
        @click="goRegister"
      >
        {{ t('login.guestPrompt.goRegister') }}
      </NButton>
    </div>

    <p class="prompt-footer" @click="handleShowChange(false)">
      {{ t('login.guestPrompt.continueGuest') }}
    </p>
  </NModal>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { NModal, NButton, NIcon, useMessage } from 'naive-ui'
import { Login as LoginIcon } from '@vicons/carbon'
import { useI18n } from 'vue-i18n'
import { useLoginPrompt, closeLoginPrompt } from '@/utils/guest-action'

const router = useRouter()
const { t } = useI18n()

const { showPrompt, promptAction } = useLoginPrompt()

// 引导文案：根据触发操作（点赞/收藏/评论/加圈）拼装，无 action 时用通用文案
const actionText = computed(() => {
  const action = promptAction.value
  if (action && t(`login.guestPrompt.actions.${action}`)) {
    return t('login.guestPrompt.descWithAction', { action: t(`login.guestPrompt.actions.${action}`) })
  }
  return t('login.guestPrompt.desc')
})

// 跳登录页 / 注册页：携带 redirect，登录成功后回到当前页（保持上下文）
const currentPath = () => {
  const r = router.currentRoute.value
  return r.fullPath && r.fullPath !== '/' ? r.fullPath : '/home'
}

const goLogin = () => {
  const redirect = currentPath()
  closeLoginPrompt()
  router.push({ path: '/', query: { redirect, tab: 'login' } })
}

const goRegister = () => {
  const redirect = currentPath()
  closeLoginPrompt()
  router.push({ path: '/', query: { redirect, tab: 'register' } })
}

const handleShowChange = (val) => {
  if (!val) closeLoginPrompt()
}
</script>

<style scoped>
.prompt-icon-area {
  display: flex;
  justify-content: center;
  margin-bottom: 18px;
}

.prompt-icon-wrap {
  width: 56px;
  height: 56px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(102, 234, 194, 0.12);
  color: var(--theme-color, #66eac2);
  border: 1px solid rgba(102, 234, 194, 0.25);
}

.prompt-title {
  text-align: center;
  font-size: 20px;
  font-weight: 700;
  margin: 0 0 8px;
  color: var(--text-primary, #fff);
}

.prompt-desc {
  text-align: center;
  font-size: 14px;
  line-height: 1.6;
  margin: 0 0 24px;
  color: var(--text-secondary, rgba(255, 255, 255, 0.7));
}

.prompt-actions {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* 两个按钮统一圆角：覆盖 Naive UI 内部 --n-border-radius（默认直角/小圆角） */
.prompt-actions :deep(.n-button) {
  --n-border-radius: 14px !important;
  border-radius: 14px !important;
}

/* 主按钮：主题绿实底 + 渐变，呼应登录卡片 .gradient-btn 视觉 */
.prompt-primary-btn {
  height: 46px;
  font-size: 15px;
  font-weight: 600;
  background: var(--primary-gradient) !important;
  border: none !important;
  color: #0e1530 !important;
  box-shadow: 0 4px 16px rgba(102, 234, 194, 0.25);
  transition: all 0.2s cubic-bezier(0.4, 0, 0.2, 1) !important;
}

.prompt-primary-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 22px rgba(102, 234, 194, 0.38) !important;
}

/* 次按钮：玻璃描边 */
.prompt-secondary-btn {
  height: 46px;
  font-size: 15px;
  font-weight: 500;
  margin-top: 0 !important;
}

.prompt-footer {
  text-align: center;
  font-size: 13px;
  margin: 18px 0 0;
  color: var(--text-tertiary, rgba(255, 255, 255, 0.5));
  cursor: pointer;
  transition: color 0.2s ease;
}

.prompt-footer:hover {
  color: var(--text-secondary, rgba(255, 255, 255, 0.7));
}
</style>
