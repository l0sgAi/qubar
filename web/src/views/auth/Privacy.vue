<template>
  <div class="privacy-page">
    <AnimatedBackground />

    <div class="content-container">
      <NCard class="privacy-card" :bordered="false">
        <h1 class="page-title">{{ t('privacy.title') }}</h1>
        <div class="last-updated">{{ t('privacy.lastUpdated') }}</div>

        <div class="privacy-content">
          <section class="intro">
            <p>{{ t('privacy.intro') }}</p>
          </section>

          <section v-for="section in sections" :key="section.title" class="privacy-section">
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
                  <li v-for="(item, iIdx) in list" :key="iIdx">
                    <strong v-if="item.strong">{{ item.strong }}</strong>{{ item.text }}
                  </li>
                </ul>
              </div>
            </template>

            <ul v-for="(list, lIdx) in (section.lists || [])" :key="`l-${lIdx}`">
              <li v-for="(item, iIdx) in list" :key="iIdx">
                <strong v-if="item.strong">{{ item.strong }}</strong>{{ item.text }}
              </li>
            </ul>

            <p v-for="(paragraph, p2Idx) in (section.paragraphs2 || [])" :key="`p2-${p2Idx}`">
              {{ paragraph }}
            </p>

            <p v-if="section.warning" class="warning">{{ section.warning }}</p>
          </section>

          <section class="summary-box">
            <h3>{{ t('privacy.summaryTitle') }}</h3>
            <ul>
              <li v-for="(item, idx) in summary" :key="idx">{{ item }}</li>
            </ul>
          </section>
        </div>

        <div class="actions">
          <NButton type="primary" size="large" @click="goBack">
            {{ t('privacy.backToLogin') }}
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
const sections = computed(() => tm('privacy.sections'))
const summary = computed(() => tm('privacy.summary'))

const goBack = () => {
  router.push('/')
}
</script>

<style scoped>
.privacy-page {
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

.privacy-card {
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

.privacy-content {
  color: var(--text-secondary);
  line-height: 1.8;
  font-size: 0.95rem;
}

.intro {
  background: rgba(102, 126, 234, 0.1);
  border-left: 4px solid var(--accent-color);
  padding: 20px;
  border-radius: 8px;
  margin-bottom: 32px;
}

.intro p {
  margin: 0;
}

.privacy-section {
  margin-bottom: 32px;
}

.privacy-section h2 {
  color: var(--text-primary);
  font-size: 1.5rem;
  font-weight: 700;
  margin-bottom: 16px;
  padding-bottom: 8px;
  border-bottom: 2px solid var(--accent-color);
  display: inline-block;
}

.privacy-section h3 {
  color: var(--text-primary);
  font-size: 1.2rem;
  font-weight: 600;
  margin: 24px 0 12px 0;
}

/* 同一章节下多个子章节的容器，避免最后一个子章节的 margin 撑大章节间距 */
.privacy-section .subsection:last-child {
  margin-bottom: 0;
}

.privacy-section p {
  margin-bottom: 16px;
  text-align: justify;
}

.privacy-section strong {
  color: var(--text-primary);
  font-weight: 600;
}

.privacy-section ul {
  list-style: none;
  padding-left: 20px;
  margin-bottom: 16px;
}

.privacy-section ul li {
  position: relative;
  padding-left: 20px;
  margin-bottom: 8px;
}

.privacy-section ul li::before {
  content: '•';
  position: absolute;
  left: 0;
  color: var(--accent-color);
  font-weight: bold;
  font-size: 1.2rem;
}

.warning {
  background: rgba(255, 107, 107, 0.1);
  border-left: 4px solid #ff6b6b;
  padding: 16px;
  border-radius: 8px;
  margin: 16px 0;
}

.summary-box {
  background: rgba(102, 126, 234, 0.1);
  border: 2px solid var(--glass-border);
  border-radius: 16px;
  padding: 24px;
  margin: 32px 0;
}

.summary-box h3 {
  color: var(--text-primary);
  font-size: 1.3rem;
  font-weight: 700;
  margin-bottom: 16px;
  text-align: center;
}

.summary-box ul {
  list-style: none;
  padding: 0;
}

.summary-box ul li {
  padding: 8px 0;
  color: var(--text-secondary);
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
  .privacy-card {
    padding: 32px 24px;
  }

  .page-title {
    font-size: 2rem;
  }

  .privacy-section h2 {
    font-size: 1.3rem;
  }

  .intro,
  .summary-box {
    padding: 16px;
  }
}
</style>
