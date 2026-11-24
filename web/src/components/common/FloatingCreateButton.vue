<template>
  <div class="floating-create-button">
    <NButton
      size="large"
      type="primary"
      class="create-button"
      @click="handleClick"
    >
      <template #icon>
        <NIcon size="20">
          <AddIcon />
        </NIcon>
      </template>
      {{ t('circle.createPost') }}
    </NButton>
  </div>
</template>

<script setup>
import { useRouter } from 'vue-router'
import { NButton, NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { AddCircleOutlineFilled as AddIcon } from '@vicons/material'

const router = useRouter()
const { t } = useI18n()

const handleClick = () => {
  router.push('/create-post')
}
</script>

<style scoped>
.floating-create-button {
  position: fixed;
  left: 16dvw;
  bottom: 65dvh;
  z-index: 1000;
}

.create-button {
  box-shadow: 0 4px 20px rgba(24, 160, 88, 0.4);
  background: linear-gradient(135deg, #18a058 0%, #36ad6a 100%);
  border: none;
  border-radius: 12px;
  transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
  animation: float 3s ease-in-out infinite;
  padding: 0 24px;
  font-weight: 500;
}

.create-button:hover {
  transform: translateY(-2px);
  box-shadow: 0 6px 28px rgba(24, 160, 88, 0.5);
}

.create-button:active {
  transform: translateY(0);
}

@keyframes float {
  0%, 100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(-8px);
  }
}

/* 响应式 */
@media (max-width: 1200px) {
  .floating-create-button {
    right: 40px;
  }
}

@media (max-width: 768px) {
  .floating-create-button {
    right: 20px;
    bottom: 20px;
  }

  .create-button {
    padding: 0 20px;
  }
}
</style>
