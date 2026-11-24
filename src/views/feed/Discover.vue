<template>
  <div class="discover-page">
    <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell） -->
    <AppShell v-model:offset="offset" />

    <!-- 主内容区域和右侧栏的容器 -->
    <div class="content-wrapper" :style="contentStyle">
      <!-- 主内容区域 -->
      <div class="main-content">
        <div class="discover-container">
          <MobileFeedSegment v-if="mobileShell" />
          <!-- 顶部：标题 + 视图模式切换 + 换一批 -->
          <div class="discover-header">
            <div class="header-left">
              <h1 class="page-title"> {{ t('discover.title') }}</h1>
              <span class="page-subtitle">{{ t('discover.subtitle') }}</span>
            </div>
            <div class="header-right">
              <!-- 仅渲染 tab 栏（NTab 不生成内容面板），样式与首页/个人页的 line 式 Tab 保持一致 -->
              <NTabs v-model:value="mode" type="line" size="small" class="mode-tabs" @update:value="handleModeChange">
                <NTab name="stream">
                  <span class="mode-btn"><StreamIcon />{{ t('discover.modeStream') }}</span>
                </NTab>
                <NTab name="sectioned">
                  <span class="mode-btn"><GridIcon />{{ t('discover.modeSectioned') }}</span>
                </NTab>
                <NTab name="wall">
                  <span class="mode-btn"><WallIcon />{{ t('discover.modeWall') }}</span>
                </NTab>
              </NTabs>
              <NButton size="small" secondary class="refresh-btn" :loading="firstLoading" @click="refresh">
                <template #icon><RefreshIcon /></template>
                {{ firstLoading ? t('discover.refreshing') : t('discover.refresh') }}
              </NButton>
            </div>
          </div>

          <!-- 首屏加载 -->
          <div v-if="firstLoading" class="first-loading">
            <NSpin size="medium" />
          </div>

          <!-- 全空态 -->
          <div v-else-if="isEmpty" class="discover-empty">{{ t('discover.empty') }}</div>

          <!-- ============ 探索流 Stream ============ -->
          <div v-else-if="mode === 'stream'" class="stream">
            <template v-for="(item, i) in streamItems" :key="`${item.type}-${item.data.id || item.data.postId || i}`">
              <PostCard v-if="item.type === 'post'" v-bind="item.data" />
              <DiscoverCircleCard v-else :circle="item.data" variant="mini" />
            </template>
            <div ref="postsSentinel" class="sentinel"></div>
            <div v-if="postsLoading" class="load-more-text">{{ t('common.loading') }}</div>
            <div v-else-if="!postsHasMore && !circlesHasMore" class="load-more-text">{{ t('common.noMore') }}</div>
          </div>

          <!-- ============ 分区探索 Sectioned ============ -->
          <div v-else-if="mode === 'sectioned'" class="sectioned">
            <!-- 圈子网格 -->
            <section class="d-section">
              <div class="section-title">
                {{ t('discover.circlesSection') }}
                <span v-if="circles.length" class="section-count">{{ circles.length }}</span>
              </div>
              <div v-if="circles.length" class="circle-grid">
                <DiscoverCircleCard
                  v-for="c in circles"
                  :key="`circle-${c.id}`"
                  :circle="c"
                  variant="card"
                />
              </div>
              <div v-else class="section-sub-empty">{{ t('discover.empty') }}</div>
              <div ref="circlesSentinel" class="sentinel"></div>
              <div v-if="circlesLoading" class="load-more-text">{{ t('common.loading') }}</div>
              <div v-else-if="!circlesHasMore && circles.length" class="load-more-text">{{ t('common.noMore') }}</div>
            </section>

            <!-- 帖子流 -->
            <section class="d-section">
              <div class="section-title">
                {{ t('discover.postsSection') }}
                <span v-if="posts.length" class="section-count">{{ posts.length }}</span>
              </div>
              <PostList v-if="posts.length" :posts="posts" />
              <div v-else class="section-sub-empty">{{ t('discover.empty') }}</div>
              <div ref="postsSentinel" class="sentinel"></div>
              <div v-if="postsLoading" class="load-more-text">{{ t('common.loading') }}</div>
              <div v-else-if="!postsHasMore && posts.length" class="load-more-text">{{ t('common.noMore') }}</div>
            </section>
          </div>

          <!-- ============ 灵感墙 Wall ============ -->
          <div v-else class="wall">
            <div class="wall-grid">
              <template v-for="(item, i) in wallItems" :key="`wall-${item.type}-${item.data.id || item.data.postId || i}`">
                <DiscoverPostTile v-if="item.type === 'post'" :post="item.data" />
                <DiscoverCircleCard v-else :circle="item.data" variant="card" />
              </template>
            </div>
            <div ref="postsSentinel" class="sentinel"></div>
            <div v-if="postsLoading || circlesLoading" class="load-more-text">{{ t('common.loading') }}</div>
            <div v-else-if="!postsHasMore && !circlesHasMore" class="load-more-text">{{ t('common.noMore') }}</div>
          </div>
        </div>
      </div>

      <!-- 右侧信息栏（匿名态隐藏，主内容自动占满） -->
      <RightSidebar v-if="isLoggedIn && !mobileShell" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount, h } from 'vue'
import { NTabs, NTab, NButton, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import AppShell from '@/components/layout/AppShell.vue'
import MobileFeedSegment from '@/components/layout/mobile/MobileFeedSegment.vue'
import { useAppShell } from '@/composables/useAppShell'
import RightSidebar from '@/components/layout/RightSidebar.vue'
import PostCard from '@/components/post/PostCard.vue'
import PostList from '@/components/post/PostList.vue'
import DiscoverCircleCard from '@/components/feed/DiscoverCircleCard.vue'
import DiscoverPostTile from '@/components/feed/DiscoverPostTile.vue'
import { getDiscover } from '@/api/discover'
import { auth } from '@/utils/auth'

const message = useMessage()
const { t } = useI18n()
// 侧栏宽度与内容区偏移（移动端外框下不偏移），见 composables/useAppShell.js
const { offset, mobileShell, contentStyle } = useAppShell()

const isLoggedIn = computed(() => auth.isAuthenticated())

const PAGE_SIZE = 20
const mode = ref('stream') // 'stream' | 'sectioned' | 'wall'

// 圈子分区状态（保留原始 snake_case，供 DiscoverCircleCard 直接用）
const circles = ref([])
const circlesOffset = ref(0)
const circlesToken = ref('')
const circlesHasMore = ref(false)
const circlesLoading = ref(false)

// 帖子分区状态（transformPost 转 camelCase，供 PostCard/PostList/Tile 用）
const posts = ref([])
const postsOffset = ref(0)
const postsToken = ref('')
const postsHasMore = ref(false)
const postsLoading = ref(false)

const firstLoading = ref(true)
const isEmpty = computed(() => !circles.value.length && !posts.value.length)

// 探索流：帖子为主干，每隔几条插入一张「圈子」卡片（打破信息气泡），多余圈子追加到末尾
const streamItems = computed(() => {
  const out = []
  let ci = 0
  const STEP = 3
  posts.value.forEach((p, i) => {
    out.push({ type: 'post', data: p })
    if ((i + 1) % STEP === 0 && ci < circles.value.length) {
      out.push({ type: 'circle', data: circles.value[ci++] })
    }
  })
  while (ci < circles.value.length) out.push({ type: 'circle', data: circles.value[ci++] })
  return out
})

// 卡片墙：帖子方块与圈子卡片交替混排成统一网格
const wallItems = computed(() => {
  const out = []
  const max = Math.max(posts.value.length, circles.value.length)
  for (let i = 0; i < max; i++) {
    if (i < posts.value.length) out.push({ type: 'post', data: posts.value[i] })
    if (i < circles.value.length) out.push({ type: 'circle', data: circles.value[i] })
  }
  return out
})

// 后端 snake_case → 组件 camelCase（与首页信息流帖子项字段完全一致，照搬 Home.vue）
const transformPost = (p) => {
  const ts = new Date(p.create_time || '').getTime()
  return {
    postId: p.id,
    circleId: p.circle_id,
    circleName: p.circle_name || '',
    circleAvatar: p.circle_avatar || '',
    userId: p.user_id,
    userName: p.author_name || '',
    userAvatar: p.author_avatar || '',
    title: p.title || '',
    content: p.summary || p.content || '',
    images: p.images || [],
    postTime: isNaN(ts) ? Date.now() : ts,
    viewCount: p.view_count || 0,
    likeCount: p.like_count || 0,
    commentCount: p.comment_count || 0,
    collectCount: p.collect_count || 0,
    isLiked: !!p.is_liked,
    isCollected: !!p.is_collected,
    shareCount: p.share_count ?? null,
    isShared: !!p.is_shared
  }
}

// 首屏聚合：section=all，两分区各 size 条
const fetchFirst = async () => {
  firstLoading.value = true
  try {
    const res = await getDiscover({ section: 'all', size: PAGE_SIZE })
    const data = res.data || {}
    const cList = data.circles || []
    const pList = (data.posts || []).map(transformPost)
    circles.value = cList
    posts.value = pList
    circlesToken.value = data.pool_token || ''
    postsToken.value = data.pool_token || ''
    // offset 是候选池切片位置，不是已展示条数：私密圈 / 失效帖子在回填时被过滤（后端 #170），
    // 单页可能不足 size 条，下一页仍从 offset + size 开始，否则会重复取到同一段池
    circlesOffset.value = PAGE_SIZE
    postsOffset.value = PAGE_SIZE
    // section=all 只有一个 has_more，不能分别对应两个分区：首屏有内容或接口说还有更多时
    // 先视为可能还有更多，不能按「本页满 size」推断；第一次单分区翻页后以真实 has_more 纠正
    circlesHasMore.value = cList.length > 0 || !!data.has_more
    postsHasMore.value = pList.length > 0 || !!data.has_more
  } catch (e) {
    console.error('获取发现内容失败:', e)
    message.error(e.message || t('discover.loadFailed'))
    circles.value = []
    posts.value = []
    circlesHasMore.value = false
    postsHasMore.value = false
  } finally {
    firstLoading.value = false
  }
}

// 圈子分区翻页：section=circles + offset + 回传 pool_token
const loadMoreCircles = async () => {
  if (circlesLoading.value || !circlesHasMore.value) return
  circlesLoading.value = true
  try {
    const res = await getDiscover({
      section: 'circles',
      size: PAGE_SIZE,
      offset: circlesOffset.value,
      pool_token: circlesToken.value
    })
    const data = res.data || {}
    const list = data.circles || []
    if (data.pool_refreshed) {
      // 池已重建：本次是 offset=0 的新池首页，重置本地列表
      circles.value = list
      message.info(t('discover.refreshed'))
    } else {
      circles.value = [...circles.value, ...list]
    }
    circlesToken.value = data.pool_token || ''
    circlesOffset.value = data.pool_refreshed ? PAGE_SIZE : (circlesOffset.value + PAGE_SIZE)
    circlesHasMore.value = !!data.has_more
  } catch (e) {
    console.error('发现圈子翻页失败:', e)
    message.error(e.message || t('discover.loadFailed'))
  } finally {
    circlesLoading.value = false
  }
}

// 帖子分区翻页：section=posts + offset + 回传 pool_token
const loadMorePosts = async () => {
  if (postsLoading.value || !postsHasMore.value) return
  postsLoading.value = true
  try {
    const res = await getDiscover({
      section: 'posts',
      size: PAGE_SIZE,
      offset: postsOffset.value,
      pool_token: postsToken.value
    })
    const data = res.data || {}
    const list = (data.posts || []).map(transformPost)
    if (data.pool_refreshed) {
      posts.value = list
      message.info(t('discover.refreshed'))
    } else {
      posts.value = [...posts.value, ...list]
    }
    postsToken.value = data.pool_token || ''
    postsOffset.value = data.pool_refreshed ? PAGE_SIZE : (postsOffset.value + PAGE_SIZE)
    postsHasMore.value = !!data.has_more
  } catch (e) {
    console.error('发现帖子翻页失败:', e)
    message.error(e.message || t('discover.loadFailed'))
  } finally {
    postsLoading.value = false
  }
}

// 「换一批」：重置状态后重新首屏聚合（后台约 10 分钟才重建池，期间返回同一批）
const refresh = () => {
  if (firstLoading.value) return
  circlesToken.value = ''
  postsToken.value = ''
  fetchFirst()
}

// 模式切换是纯视图变化，不重新请求（同一批数据换个排法）
const handleModeChange = () => {}

/* ---------- 无限滚动 ---------- */
const postsSentinel = ref(null)
const circlesSentinel = ref(null)
let observers = []

const disconnectAll = () => {
  observers.forEach((o) => o.disconnect())
  observers = []
}

// stream/wall 的底部哨兵：帖子优先，帖子耗尽则补圈子（两种模式都是帖子+圈子混排）
const onMainIntersect = () => {
  if (!postsHasMore.value && circlesHasMore.value) {
    return loadMoreCircles()
  }
  if (postsHasMore.value) return loadMorePosts()
}

const attach = (el, handler, canLoad) => {
  if (!el) return
  const obs = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting && canLoad()) handler()
    },
    { rootMargin: '300px' }
  )
  obs.observe(el)
  observers.push(obs)
}

const setupObservers = () => {
  disconnectAll()
  if (firstLoading.value || isEmpty.value) return
  if (mode.value === 'sectioned') {
    attach(circlesSentinel.value, loadMoreCircles, () => circlesHasMore.value && !circlesLoading.value)
    attach(postsSentinel.value, loadMorePosts, () => postsHasMore.value && !postsLoading.value)
  } else {
    // stream + wall：底部一个 postsSentinel
    attach(postsSentinel.value, onMainIntersect, () => !postsLoading.value && !circlesLoading.value && (postsHasMore.value || circlesHasMore.value))
  }
}

watch(
  [mode, postsSentinel, circlesSentinel, postsHasMore, circlesHasMore, postsLoading, circlesLoading, firstLoading, isEmpty],
  setupObservers
)

onMounted(() => {
  // 发现页可选登录：匿名也直接拉取（纯随机），不重定向
  fetchFirst()
})

onBeforeUnmount(() => {
  disconnectAll()
})

/* ---------- 视图模式图标（inline SVG） ---------- */
const svg = (d) => () => h('svg', {
  viewBox: '0 0 24 24', fill: 'none', stroke: 'currentColor', 'stroke-width': '2', style: 'width:14px;height:14px'
}, [h('path', { d })])
const StreamIcon = svg('M4 6h16M4 12h16M4 18h10')
const GridIcon = svg('M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z')
const WallIcon = svg('M4 4h4v6H4zM10 4h10v3H10zM10 9h10v3H10zM4 12h4v8H4zM10 14h10v6H10z')
const RefreshIcon = svg('M23 4v6h-6M1 20v-6h6M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15')
</script>

<style scoped>
.discover-page {
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
  max-width: 820px;
  width: 100%;
  box-sizing: border-box;
}

.discover-container {
  width: 100%;
  margin: 0 auto;
}

/* 顶部 */
.discover-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 20px;
}

.header-left {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.page-title {
  margin: 0;
  font-size: 22px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.95);
}

.page-subtitle {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.45);
}

.header-right {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

/* —— 顶部切换 Tab：line 式下划线风格，与首页 / 个人页的切换 Tab 保持一致 —— */
.header-right .mode-tabs {
  flex: none;
  width: auto;
}

/* 收紧 tab 间距与内边距，使其与右侧刷新按钮在标题栏内对齐 */
.header-right .mode-tabs :deep(.n-tabs-nav) {
  line-height: 1.5;
}

.header-right .mode-tabs :deep(.n-tabs-tab) {
  padding: 4px 10px;
}

/* —— 刷新按钮：胶囊圆角 + 主题色描边/文字 —— */
.header-right :deep(.refresh-btn) {
  border-radius: 999px !important;
  color: #60F8BB !important;
  border: 1px solid rgba(96, 248, 187, 0.35) !important;
  background: rgba(96, 248, 187, 0.08) !important;
}

@media (hover: hover) {
  .header-right :deep(.refresh-btn:hover) {
    background: rgba(96, 248, 187, 0.16) !important;
    border-color: rgba(96, 248, 187, 0.55) !important;
  }
}

.header-right :deep(.refresh-btn .n-button__content) {
  color: #60F8BB !important;
}

.mode-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
}

.first-loading {
  display: flex;
  justify-content: center;
  padding: 80px 0;
}

.discover-empty {
  padding: 96px 24px;
  text-align: center;
  color: rgba(255, 255, 255, 0.4);
  font-size: 0.95rem;
  line-height: 1.6;
}

.sentinel {
  height: 1px;
}

.load-more-text {
  text-align: center;
  color: rgba(255, 255, 255, 0.4);
  font-size: 0.85rem;
  padding: 20px 0;
}

/* 探索流：单列混排，统一间距（抹掉 PostCard 自带的 margin-bottom，交给容器 gap） */
.stream {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.stream :deep(.post-card),
.stream :deep(.m-post-item) {
  margin-bottom: 0;
}

@media (max-width: 768px) {
  .stream {
    gap: 8px;
  }
}

/* 分区模式 */
.sectioned {
  display: flex;
  flex-direction: column;
  gap: 36px;
}

.d-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.section-title {
  font-size: 16px;
  font-weight: 700;
  color: rgba(255, 255, 255, 0.9);
  padding: 4px 2px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.section-title::before {
  content: '';
  width: 4px;
  height: 16px;
  border-radius: 2px;
  background: var(--primary-gradient);
}

.section-count {
  margin-left: 4px;
  font-size: 12px;
  font-weight: 600;
  color: #60F8BB;
  background: rgba(96, 248, 187, 0.12);
  border-radius: 999px;
  padding: 1px 9px;
  line-height: 1.6;
}

.section-sub-empty {
  padding: 32px 0;
  text-align: center;
  color: rgba(255, 255, 255, 0.35);
  font-size: 0.85rem;
}

/* 圈子网格 */
.circle-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 14px;
}

/* 灵感墙 */
.wall-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
  gap: 14px;
  align-items: stretch;
}

/* 响应式 */
@media (max-width: 1200px) {
  .content-wrapper {
    flex-direction: column;
  }

  .right-sidebar {
    display: none;
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

  .circle-grid {
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
  }

  .wall-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>
