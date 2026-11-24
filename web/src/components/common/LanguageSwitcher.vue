<template>
  <AppDropdown
    :options="languageOptions"
    :render-label="renderLabel"
    @select="handleLanguageChange"
  >
    <NButton quaternary circle size="small" class="lang-trigger">
      <template #icon>
        <NIcon size="18">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"/>
            <line x1="2" y1="12" x2="22" y2="12"/>
            <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>
          </svg>
        </NIcon>
      </template>
    </NButton>
  </AppDropdown>
</template>

<script setup>
import { computed, h } from 'vue'
import { NButton, NIcon, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import AppDropdown from '@/components/common/AppDropdown.vue'

const { locale: currentLocale } = useI18n()
const message = useMessage()

const checkIcon = () => h('svg', {
  class: 'lang-check',
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  'stroke-width': '3',
  style: { width: '14px', height: '14px' }
}, [
  h('polyline', { points: '20 6 9 17 4 12' })
])

const renderLabel = (option) => {
  return h('span', {
    class: ['lang-option', option.key === currentLocale.value ? 'is-active' : '']
  }, [
    h('span', null, option.label),
    option.key === currentLocale.value ? checkIcon() : null
  ])
}

const languageOptions = computed(() => [
  { label: '简体中文', key: 'zh-CN' },
  { label: 'English', key: 'en-US' }
])

const handleLanguageChange = (lang) => {
  currentLocale.value = lang
  localStorage.setItem('qubar_locale', lang)
  message.success(lang === 'zh-CN' ? '已切换至中文' : 'Switched to English')
}
</script>

<style scoped>
.lang-trigger {
  color: rgba(255, 255, 255, 0.85);
  transition: color 0.25s ease, background 0.25s ease !important;
}

.lang-trigger:hover {
  color: #8af0d0 !important;
  background: rgba(102, 234, 194, 0.12) !important;
}
</style>
