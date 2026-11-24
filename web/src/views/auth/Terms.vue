<template>
  <div class="terms-page">
    <AnimatedBackground />

    <div class="content-container">
      <NCard class="terms-card" :bordered="false">
        <h1 class="page-title">{{ t('terms.title') }}</h1>
        <div class="last-updated">{{ t('terms.lastUpdated') }}</div>

        <div class="terms-content">
          <section v-for="section in sections" :key="section.title" class="terms-section">
            <h2>{{ section.title }}</h2>

            <p v-for="(paragraph, pIdx) in (section.paragraphs || [])" :key="`p-${pIdx}`">
              {{ paragraph }}
            </p>

            <template v-if="section.subsections">
              <div v-for="sub in section.subsections" :key="sub.title" class="subsection">
                <h3>{{ sub.title }}</h3>
                <p v-for="(paragraph, spIdx) in (sub.paragraphs || [])" :key="`sp-${spIdx}`">
                  {{ paragraph }}
                </p>
                <ul v-for="(list, lIdx) in (sub.lists || [])" :key="`sl-${lIdx}`">
                  <li v-for="(item, iIdx) in list" :key="iIdx">{{ item }}</li>
                </ul>
              </div>
            </template>

            <ul v-for="(list, lIdx) in (section.lists || [])" :key="`l-${lIdx}`">
              <li v-for="(item, iIdx) in list" :key="iIdx">{{ item }}</li>
            </ul>
          </section>
        </div>

        <div class="actions">
          <NButton type="primary" size="large" @click="goBack">
            {{ t('terms.backToLogin') }}
          </NButton>
        </div>
      </NCard>
    </div>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { NCard, NButton } from 'naive-ui'
import AnimatedBackground from '@/components/auth/AnimatedBackground.vue'

const router = useRouter()
const { t, tm } = useI18n()

// tm() 返回原始数据结构，随 locale 切换自动响应
const sections = computed(() => tm('terms.sections'))

const goBack = () => {
  router.push('/')
}
</script>

<style scoped>
.terms-page {
  min-height: 100vh;
  position: relative;
  padding: 40px 20px;
  overflow-y: auto;
}

.content-container {
  position: relative;
  z-index: 1;
  max-width: 900px;
  margin: 0 auto;
}

.terms-card {
  background: var(--glass-bg);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border: 1px solid var(--glass-border);
  border-radius: 24px;
  box-shadow: var(--shadow-md);
  padding: 48px;
}

.page-title {
  font-size: 2.5rem;
  font-weight: 800;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  margin-bottom: 12px;
  text-align: center;
}

.last-updated {
  text-align: center;
  color: var(--text-tertiary);
  font-size: 0.9rem;
  margin-bottom: 40px;
  padding-bottom: 20px;
  border-bottom: 1px solid var(--glass-border);
}

.terms-content {
  color: var(--text-secondary);
  line-height: 1.8;
  font-size: 0.95rem;
}

.terms-section {
  margin-bottom: 32px;
}

.terms-section h2 {
  color: var(--text-primary);
  font-size: 1.5rem;
  font-weight: 700;
  margin-bottom: 16px;
  padding-bottom: 8px;
  border-bottom: 2px solid var(--accent-color);
  display: inline-block;
}

.terms-section h3 {
  color: var(--text-primary);
  font-size: 1.2rem;
  font-weight: 600;
  margin: 24px 0 12px 0;
}

/* 同一章节下多个子章节的容器，避免最后一个子章节的 margin 撑大章节间距 */
.terms-section .subsection:last-child {
  margin-bottom: 0;
}

.terms-section p {
  margin-bottom: 16px;
  text-align: justify;
}

.terms-section ul {
  list-style: none;
  padding-left: 20px;
  margin-bottom: 16px;
}

.terms-section ul li {
  position: relative;
  padding-left: 20px;
  margin-bottom: 8px;
}

.terms-section ul li::before {
  content: '•';
  position: absolute;
  left: 0;
  color: var(--accent-color);
  font-weight: bold;
  font-size: 1.2rem;
}

.actions {
  margin-top: 48px;
  padding-top: 32px;
  border-top: 1px solid var(--glass-border);
  display: flex;
  justify-content: center;
  gap: 16px;
}

:deep(.n-card) {
  background: transparent !important;
}

@media (max-width: 768px) {
  .terms-card {
    padding: 32px 24px;
  }

  .page-title {
    font-size: 2rem;
  }

  .terms-section h2 {
    font-size: 1.3rem;
  }
}
</style>
