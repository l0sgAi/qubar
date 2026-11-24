<template>
  <div class="hot-page">
    <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell） -->
    <AppShell v-model:offset="offset" />

    <!-- 主内容区域和右侧栏的容器 -->
    <div class="content-wrapper" :style="contentStyle">
      <!-- 主内容区域 -->
      <div class="main-content">
        <div class="hot-container">
          <MobileFeedSegment v-if="mobileShell" />
          <!-- 顶部：标题 + 时间窗切换 + 更新时间 -->
          <div class="hot-header">
            <div class="header-left">
              <h1 class="page-title">{{ t('trending.title') }}</h1>
              <span class="refreshed-label" :class="{ 'is-live': refreshedAt > 0 }">
                <i class="live-dot"></i>{{ refreshedLabel }}
              </span> 
            </div>
            <NButtonGroup size="small" class="window-switch">
              <NButton round :class="{ 'is-active': window === '24h' }" @click="switchWindow('24h')">
                {{ t('trending.window24h') }}
              </NButton>
              <NButton round :class="{ 'is-active': window === '7d' }" @click="switchWindow('7d')">
                {{ t('trending.window7d') }}
              </NButton>
            </NButtonGroup>
          </div>

          <div v-if="!loading && loadFailed" class="hot-empty" role="alert">
            <p>{{ t('trending.loadFailed') }}</p>
            <NButton text @click="fetchTrending">{{ t('common.retry') }}</NButton>
          </div>

          <!-- 全空态 -->
          <div v-else-if="!loading && isEmpty" class="hot-empty">
              {{ t('trending.empty') }}
          </div>

          <!-- 三段榜单 -->
          <template v-else>
            <section v-for="sec in sections" :key="sec.type" class="trending-section">
              <div class="section-title">
                <span class="section-icon" aria-hidden="true">
                  <svg viewBox="0 0 24 24"><path fill="currentColor" :d="sec.icon" /></svg>
                </span>
                <span class="section-label">{{ t(sec.labelKey) }}</span>
                <span class="section-line"></span>
              </div>

              <!-- 骨架 -->
              <div v-if="loading" class="section-list">
                <div v-for="i in 5" :key="i" class="skel-row">
                  <div class="skel skel-rank"></div>
                  <div class="skel skel-avatar"></div>
                  <div class="skel-text">
                    <div class="skel skel-line"></div>
                    <div class="skel skel-line short"></div>
                  </div>
                  <div class="skel skel-hot"></div>
                </div>
              </div>

              <!-- 列表 -->
              <div v-else-if="sec.list.length" class="section-list">
                <TrendingCard
                  v-for="(item, idx) in sec.list"
                  :key="`${sec.type}-${item.id || idx}`"
                  :type="sec.type"
                  :rank="idx + 1"
                  :item="item"
                />
              </div>
            </section>
          </template>
        </div>
      </div>

      <!-- 右侧信息栏（匿名态隐藏，主内容自动占满；与 Home.vue / Discover.vue 一致） -->
      <RightSidebar v-if="isLoggedIn && !mobileShell" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onBeforeUnmount } from 'vue'
import { useRouter } from 'vue-router'
import { NButtonGroup, NButton, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import AppShell from '@/components/layout/AppShell.vue'
import MobileFeedSegment from '@/components/layout/mobile/MobileFeedSegment.vue'
import { useAppShell } from '@/composables/useAppShell'
import RightSidebar from '@/components/layout/RightSidebar.vue'
import TrendingCard from '@/components/feed/TrendingCard.vue'
import { getTrending } from '@/api/trending'
import { auth } from '@/utils/auth'

const router = useRouter()
const message = useMessage()
const { t } = useI18n()
// 侧栏宽度与内容区偏移（移动端外框下不偏移），见 composables/useAppShell.js
const { offset, mobileShell, contentStyle } = useAppShell()

const isLoggedIn = computed(() => auth.isAuthenticated())

// 首屏聚合：section=all，每类 size=10，不做翻页
const SECTION_SIZE = 10
const window = ref('24h')

// 三段榜单的段标题图标（Material 风格填充路径）
const SECTION_ICONS = {
  post: 'M20 2H4c-1.1 0-2 .9-2 2v18l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2z',
  circle: 'M16 11c1.66 0 3-1.34 3-3s-1.34-3-3-3-3 1.34-3 3 1.34 3 3 3zm-8 0c1.66 0 3-1.34 3-3S9.66 5 8 5 5 6.34 5 8s1.34 3 3 3zm0 2c-2.33 0-7 1.17-7 3.5V19h14v-2.5c0-2.33-4.67-3.5-7-3.5zm8 0c-.29 0-.62.02-.97.05 1.16.84 1.97 1.97 1.97 3.45V19h6v-2.5c0-2.33-4.67-3.5-7-3.5z',
  user: 'M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z'
}

const posts = ref([])
const circles = ref([])
const users = ref([])
const refreshedAt = ref(0)
const loading = ref(false)
const loadFailed = ref(false)

// 拉取世代：切时间窗时丢弃过期响应
let fetchGen = 0

// 三段配置（顺序：帖子 → 圈子 → 用户）
const sections = computed(() => [
  { type: 'post', labelKey: 'trending.posts', icon: SECTION_ICONS.post, list: posts.value },
  { type: 'circle', labelKey: 'trending.circles', icon: SECTION_ICONS.circle, list: circles.value },
  { type: 'user', labelKey: 'trending.users', icon: SECTION_ICONS.user, list: users.value }
])

const isEmpty = computed(() =>
  !posts.value.length && !circles.value.length && !users.value.length
)

// 「X 分钟前更新」文案 —— refreshedAt 为 Unix 秒；0 = 从未刷新
const nowMs = ref(Date.now())
let ticker = null
const refreshedLabel = computed(() => {
  const ts = refreshedAt.value
  if (!ts) return t('trending.refreshedNever')
  const diffMin = Math.floor((nowMs.value - ts * 1000) / 60000)
  if (diffMin < 1) return t('trending.refreshedJustNow')
  if (diffMin < 60) return t('trending.refreshedMinutesAgo', { n: diffMin })
  const diffH = Math.floor(diffMin / 60)
  return t('trending.refreshedHoursAgo', { n: diffH })
})

const fetchTrending = async () => {
  fetchGen++
  const gen = fetchGen
  loading.value = true
  loadFailed.value = false
  try {
    const res = await getTrending({ window: window.value, section: 'all', size: SECTION_SIZE })
    if (gen !== fetchGen) return
    const data = res.data || {}
    posts.value = data.posts || []
    circles.value = data.circles || []
    users.value = data.users || []
    refreshedAt.value = data.refreshed_at || 0
  } catch (e) {
    if (gen !== fetchGen) return
    loadFailed.value = true
    console.error('获取热点榜单失败:', e)
    message.error(e.message || t('trending.loadFailed'))
    posts.value = []
    circles.value = []
    users.value = []
    // 列表已清空，live 指示器同步熄灭，避免误显示旧刷新时间
    refreshedAt.value = 0
  } finally {
    if (gen === fetchGen) loading.value = false
  }
}

const handleWindowChange = () => {
  posts.value = []
  circles.value = []
  users.value = []
  fetchTrending()
}

// 按钮组没有 radio 的受控语义，切换时手动赋值再触发刷新
const switchWindow = (v) => {
  if (window.value === v) return
  window.value = v
  handleWindowChange()
}

onMounted(() => {
  // 热点页访客可读：/trending 已开放 anonymous，无需登录检查
  fetchTrending()
  // 每分钟刷新一次「X 分钟前」文案
  ticker = setInterval(() => { nowMs.value = Date.now() }, 60000)
})

onBeforeUnmount(() => {
  if (ticker) clearInterval(ticker)
})
</script>

<style scoped>
.hot-page {
  min-height: 100vh;
}

.content-wrapper {
  display: flex;
  justify-content: center;
  gap: 24px;
  margin-top: var(--header-height);
  min-height: calc(100vh - var(--header-height));
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.main-content {
  padding: 24px;
  min-height: calc(100vh - var(--header-height));
  max-width: 780px;
  width: 100%;
  box-sizing: border-box;
}

.hot-container {
  width: 100%;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 24px;
}

/* 顶部 */
.hot-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* 页面身份图标：主题绿渐变方块 + 白火焰 */
.title-flame {
  width: 34px;
  height: 34px;
  border-radius: 10px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--primary-gradient);
  color: rgba(255, 255, 255, 0.95);
  /* box-shadow: 0 4px 14px rgba(102, 234, 194, 0.3);
  flex-shrink: 0; */
}

.title-flame svg {
  width: 18px;
  height: 18px;
}

.page-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.95);
}

/* 时间窗切换：NButtonGroup 圆角胶囊分段控件（内联渲染，scoped :deep 可命中）。
   组合按钮自身不定高、按钮光环层 border-radius: inherit，
   外侧圆角由 round 提供、相邻内侧圆角由 naive 内置 !important 压平，
   不再与自定义样式争抢优先级 */
.window-switch {
  padding: 3px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
}

/* 去掉按钮自带边框，外框统一由胶囊容器提供。
   naive 的 hover/focus/pressed 边框规则带伪类、优先级更高，单条 !important 统一压掉 */
.window-switch :deep(.n-button__border),
.window-switch :deep(.n-button__state-border) {
  border: none !important;
}

.window-switch :deep(.n-button) {
  padding: 0 14px;
  transition: background-color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

/* 未选中 hover：白色微亮底（naive 默认类型各状态底色本就是透明，无冲突） */
@media (hover: hover) {
  .window-switch :deep(.n-button:not(.is-active):hover) {
    background-color: rgba(255, 255, 255, 0.06);
  }
}

/* 选中：主题绿淡底；补齐 hover/focus/active 变体，
   避免交互瞬间被 naive 同优先级的透明底色规则顶掉 */
.window-switch :deep(.n-button.is-active),
.window-switch :deep(.n-button.is-active:focus),
.window-switch :deep(.n-button.is-active:active) {
  background-color: rgba(102, 234, 194, 0.16);
}

@media (hover: hover) {
  .window-switch :deep(.n-button.is-active:hover) {
    background-color: rgba(102, 234, 194, 0.16);
  }
}

/* 文案色写在 __content 上：直接命中恒优先于 naive 设在按钮根上的 color 继承 */
.window-switch :deep(.n-button .n-button__content) {
  color: rgba(255, 255, 255, 0.55);
  font-weight: 600;
  transition: color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

@media (hover: hover) {
  .window-switch :deep(.n-button:hover .n-button__content) {
    color: rgba(255, 255, 255, 0.85);
  }
}

.window-switch :deep(.n-button.is-active .n-button__content) {
  color: #8af0d0;
}

/* 键盘可达性：焦点可见时给一圈主题绿描边 */
.window-switch :deep(.n-button:focus-visible) {
  outline: 2px solid rgba(102, 234, 194, 0.4);
  outline-offset: -2px;
}

/* 「X 分钟前更新」胶囊：有数据时绿点呼吸，无数据灰点静止 */
.refreshed-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.55);
  padding: 4px 10px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.04);
  border: 1px solid rgba(255, 255, 255, 0.08);
}

.live-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.35);
  flex-shrink: 0;
}

.refreshed-label.is-live .live-dot {
  background: #66eac2;
  box-shadow: 0 0 6px rgba(102, 234, 194, 0.8);
  animation: hot-live-pulse 2s ease-in-out infinite;
}

@keyframes hot-live-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}

/* 全空态 */
.hot-empty {
  padding: 80px 0;
  text-align: center;
  color: rgba(255, 255, 255, 0.4);
  font-size: 14px;
}

/* 段落 */
.trending-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

/* 段标题：玻璃图标块 + 标题 + 渐变延伸线 */
.section-title {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 2px;
}

.section-icon {
  width: 26px;
  height: 26px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  background: rgba(102, 234, 194, 0.1);
  border: 1px solid rgba(102, 234, 194, 0.22);
  color: #8af0d0;
  flex-shrink: 0;
}

.section-icon svg {
  width: 14px;
  height: 14px;
}

.section-label {
  font-size: 15px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.88);
}

.section-line {
  flex: 1;
  height: 1px;
  background: linear-gradient(90deg, rgba(102, 234, 194, 0.3), rgba(102, 234, 194, 0.04));
}

.section-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

/* 列表进场 stagger：逐项延迟 40ms，弹性回弹进入（离开不做动画，列表为整体换血） */
.section-list > * {
  animation: hot-item-in 0.25s cubic-bezier(0.34, 1.56, 0.64, 1) backwards;
}

.section-list > *:nth-child(1) { animation-delay: 0.02s; }
.section-list > *:nth-child(2) { animation-delay: 0.06s; }
.section-list > *:nth-child(3) { animation-delay: 0.1s; }
.section-list > *:nth-child(4) { animation-delay: 0.14s; }
.section-list > *:nth-child(5) { animation-delay: 0.18s; }
.section-list > *:nth-child(6) { animation-delay: 0.22s; }
.section-list > *:nth-child(7) { animation-delay: 0.26s; }
.section-list > *:nth-child(8) { animation-delay: 0.3s; }
.section-list > *:nth-child(9) { animation-delay: 0.34s; }
.section-list > *:nth-child(10) { animation-delay: 0.38s; }

@keyframes hot-item-in {
  from {
    opacity: 0;
    transform: translateY(8px);
  }
  to {
    opacity: 1;
    transform: translateY(0);
  }
}

/* 骨架 */
.skel-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px;
  border-radius: 14px;
  background: rgba(255, 255, 255, 0.03);
}

.skel {
  background: linear-gradient(90deg, rgba(255,255,255,0.06) 25%, rgba(255,255,255,0.16) 37%, rgba(255,255,255,0.06) 63%);
  background-size: 400% 100%;
  animation: hot-skel-shimmer 1.4s ease infinite;
  border-radius: 6px;
}

.skel-rank { width: 18px; height: 18px; border-radius: 6px; flex-shrink: 0; }
.skel-avatar { width: 44px; height: 44px; border-radius: 50%; flex-shrink: 0; }
.skel-text { flex: 1; display: flex; flex-direction: column; gap: 8px; }
.skel-line { height: 12px; width: 60%; }
.skel-line.short { width: 35%; }
.skel-hot { width: 48px; height: 16px; flex-shrink: 0; }

/* 响应式（与 Home.vue / Discover.vue 保持一致） */
@media (max-width: 1200px) {
  .content-wrapper {
    flex-direction: column;
  }
}

@media (max-width: 768px) {
  .content-wrapper {
    margin-left: 0;
    padding: 0;
  }

  .main-content {
    padding: 16px;
  }
}
</style>

<!-- 骨架 shimmer 关键帧（全局：骨架元素 class 无 scoped data-v） -->
<style>
@keyframes hot-skel-shimmer {
  0% { background-position: 100% 50%; }
  100% { background-position: 0 50%; }
}
</style>
