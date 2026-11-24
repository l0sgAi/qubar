<template>
  <Transition name="card-slide">
    <div v-if="circleData" class="circle-rule-card">
      <NCard :bordered="false" class="rule-card">
        <div class="circle-header">
          <div class="circle-avatar-wrapper">
            <img
              v-if="circleData.avatar_url"
              :src="circleData.avatar_url"
              class="circle-avatar"
              :alt="circleData.name"
            />
            <div v-else class="circle-avatar-placeholder">
              {{ circleData.name?.charAt(0)?.toUpperCase() || '?' }}
            </div>
          </div>
          <div class="circle-info">
            <h3 class="circle-name">{{ circleData.name }}</h3>
            <p class="circle-slug">c/{{ circleData.slug || circleData.id }}</p>
          </div>
        </div>

        <div v-if="circleData.rule" class="rules-section">
          <div class="rules-header">
            <NIcon size="16" class="rules-icon">
              <InfoCircleIcon />
            </NIcon>
            <span class="rules-title">{{ t('post.circleRules') }}</span>
          </div>
          <p class="rules-content">{{ circleData.rule }}</p>
        </div>

        <div v-else class="no-rules">
          <NIcon size="20" color="rgba(255,255,255,0.3)">
            <InfoCircleIcon />
          </NIcon>
          <span>{{ t('post.noCircleRules') }}</span>
        </div>
      </NCard>
    </div>
  </Transition>
</template>

<script setup>
import { NCard, NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { InfoCircle as InfoCircleIcon } from '@vicons/tabler'

const { t } = useI18n()

defineProps({
  circleData: {
    type: Object,
    default: null
  }
})
</script>

<style scoped>
.circle-rule-card {
  width: 100%;
  max-width: 360px;
}

.rule-card {
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 12px;
}

.circle-header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
  margin-bottom: 16px;
}

.circle-avatar-wrapper {
  flex-shrink: 0;
}

.circle-avatar {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.15);
  background: rgba(255, 255, 255, 0.08);
  object-fit: cover;
}

.circle-avatar-placeholder {
  width: 48px;
  height: 48px;
  border-radius: 50%;
  border: 2px solid rgba(255, 255, 255, 0.15);
  /* background: linear-gradient(135deg, #ec4899 0%, #a855f7 50%, #3b82f6 100%); */
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 1.2rem;
  font-weight: bold;
  color: white;
}

.circle-info {
  flex: 1;
  min-width: 0;
}

.circle-name {
  margin: 0 0 4px 0;
  font-size: 1rem;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.95);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.circle-slug {
  margin: 0;
  font-size: 0.75rem;
  color: rgba(255, 255, 255, 0.5);
}

.rules-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.rules-header {
  display: flex;
  align-items: center;
  gap: 6px;
}

.rules-icon {
  color: rgba(255, 255, 255, 0.5);
}

.rules-title {
  font-size: 0.85rem;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.85);
}

.rules-content {
  margin: 0;
  font-size: 0.85rem;
  color: rgba(255, 255, 255, 0.65);
  line-height: 1.6;
  white-space: pre-wrap;
  word-wrap: break-word;
  word-break: break-word;
  overflow-wrap: break-word;
}

.no-rules {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 0;
  color: rgba(255, 255, 255, 0.4);
  font-size: 0.85rem;
}

@media (max-width: 1200px) {
  .circle-rule-card {
    max-width: 100%;
    margin-top: 20px;
  }
}

/* 卡片滑入动画 */
.card-slide-enter-active {
  animation: cardSlideIn 0.4s cubic-bezier(0.16, 1, 0.3, 1);
}

.card-slide-leave-active {
  animation: cardSlideOut 0.3s ease-in;
}

@keyframes cardSlideIn {
  0% {
    opacity: 0;
    transform: translateX(20px) scale(0.95);
  }
  100% {
    opacity: 1;
    transform: translateX(0) scale(1);
  }
}

@keyframes cardSlideOut {
  0% {
    opacity: 1;
    transform: translateX(0) scale(1);
  }
  100% {
    opacity: 0;
    transform: translateX(-10px) scale(0.98);
  }
}
</style>
