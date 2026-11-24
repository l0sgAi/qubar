<template>
  <div class="not-found-page">
    <!-- 动态背景 -->
    <AnimatedBackground />

    <NCard class="not-found-container" :bordered="false">
      <div class="not-found-view">
        <div class="error-code">404</div>
        <h2 class="error-title">{{ t('notFound.title') }}</h2>
        <p class="error-desc">{{ t('notFound.description') }}</p>
        <NButton
          type="primary"
          size="large"
          class="back-btn"
          @click="handleBackHome"
        >
          {{ t('notFound.backHome') }}
        </NButton>
      </div>
    </NCard>
  </div>
</template>

<script setup>
import { NCard, NButton } from 'naive-ui'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AnimatedBackground from '@/components/auth/AnimatedBackground.vue'

const router = useRouter()
const { t } = useI18n()

// 回到信息流主页（访客可读，无需区分登录态）
const handleBackHome = () => {
  router.push('/home')
}
</script>

<style scoped>
.not-found-page {
  min-height: 100vh;
  display: flex;
  justify-content: center;
  align-items: center;
  position: relative;
}

.not-found-container {
  position: relative;
  z-index: 1;
  background: var(--glass-bg);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid var(--glass-border);
  border-radius: 24px;
  box-shadow: var(--shadow-md);
  width: 100%;
  max-width: 480px;
  margin: 0 16px;
  padding: 40px 32px;
  text-align: center;
}

.not-found-view {
  display: flex;
  flex-direction: column;
  align-items: center;
  animation: fadeIn 0.5s ease;
}

.error-code {
  font-size: 5rem;
  font-weight: 800;
  line-height: 1;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  margin-bottom: 16px;
  letter-spacing: 2px;
}

.error-title {
  color: var(--text-primary);
  margin-bottom: 12px;
  font-size: 1.5rem;
  font-weight: 700;
  letter-spacing: -0.5px;
}

.error-desc {
  color: var(--text-secondary);
  margin-bottom: 28px;
  font-size: 0.95rem;
  line-height: 1.6;
}

.back-btn {
  width: 100%;
  height: 52px;
  border-radius: 16px;
  font-size: 16px;
  font-weight: 600;
  background: var(--primary-gradient);
  border: none;
  color: white;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  box-shadow: var(--shadow-sm);
}

.back-btn:hover {
  transform: translateY(-2px);
  box-shadow: var(--shadow-md);
}

.back-btn:active {
  transform: translateY(0);
}

:deep(.n-card) {
  background: transparent !important;
}
</style>
