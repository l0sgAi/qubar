<template>
  <div class="home-page">
    <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell） -->
    <AppShell v-model:offset="offset" />

    <!-- 主内容区域和右侧栏的容器 -->
    <div class="content-wrapper" :style="contentStyle">
      <!-- 主内容区域 -->
      <div class="main-content">
        <div class="home-container">
          <div class="content-header">
            <!-- 仅渲染 tab 栏（NTab 不生成内容面板）；feed 内容在外部单份渲染，
                 切 tab 时不再 unmount/remount，避免 pane 共存闪动与组件重建 -->
            <NTabs v-model:value="activeTab" type="line" @update:value="handleTabChange">
              <NTab
                v-for="tab in TABS"
                :key="tab.name"
                :name="tab.name"
                :tab="t(tab.label)"
              />
            </NTabs>

            <!-- 内容区过渡：key 随 tab/视图变化，out-in 保证旧内容先淡出、
                 新内容再淡入，全程单份内容在 DOM，无 pane 共存闪动 -->
            <Transition name="feed-switch" mode="out-in">
              <div :key="`${activeTab}-${feedView}`" class="feed-body">
                <!-- 首屏加载：骨架屏模拟列表结构，弱网长加载时感知更流畅 -->
                <PostListSkeleton v-if="loading && !isAppending" />
                <!-- 空态：following（已加入圈子 ∪ 关注的人，后端 v0.9.0）引导加圈或关注人，其余通用空文案 -->
                <div v-else-if="posts.length === 0" class="feed-empty">
                  {{ activeTab === 'following' ? t('feed.emptyFollowing') : t('feed.empty') }}
                </div>
                <!-- 帖子列表 -->
                <PostList v-else :posts="posts" />
              </div>
            </Transition>

            <!-- 翻页加载中 / 没有更多（统一单实例，置于 tabs 之外） -->
            <template v-if="posts.length > 0">
              <div v-if="isAppending" class="feed-loading-more">
                <NSpin size="small" />
                <span>{{ t('common.loading') }}</span>
              </div>
              <div v-else-if="!hasMore" class="feed-no-more">{{ t('common.noMore') }}</div>
            </template>
            <!-- 无限滚动哨兵 -->
            <div ref="sentinel" class="feed-sentinel"></div>
          </div>
        </div>
      </div>

      <!-- 右侧信息栏（匿名态隐藏，主内容自动占满；参考 Discover.vue） -->
      <RightSidebar v-if="isLoggedIn && !mobileShell" />
    </div>
  </div>
</template>

<script setup>
import { ref, computed, watch, nextTick, onMounted, onBeforeUnmount } from 'vue'
import { NTabs, NTab, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import AppShell from '@/components/layout/AppShell.vue'
import { useAppShell } from '@/composables/useAppShell'
import PostList from '@/components/post/PostList.vue'
import PostListSkeleton from '@/components/post/PostListSkeleton.vue'
import RightSidebar from '@/components/layout/RightSidebar.vue'
import { getHomeFeed } from '@/api/post'
import { auth } from '@/utils/auth'
import { requireLogin } from '@/utils/guest-action'
import request from '@/utils/request'

const router = useRouter()
const message = useMessage()
const { t } = useI18n()
// 侧栏宽度与内容区偏移（移动端外框下不偏移），见 composables/useAppShell.js
const { offset, mobileShell, contentStyle } = useAppShell()

const isLoggedIn = computed(() => auth.isAuthenticated())

// 3 个 tab 共用 /post/home 端点（latest 已迁移至 RightSidebar）
const TABS = [
  { name: 'recommend', label: 'nav.recommend' },
  { name: 'hot', label: 'nav.hot' },
  { name: 'following', label: 'nav.following' }
]
// recommend/following 依赖登录用户数据池，访客访问会 401（'This feed tab requires login'）
const RESTRICTED_TABS = ['recommend', 'following']

const PAGE_SIZE = 20

// 统一信息流状态：切 tab 时清空本地列表 + 游标（遵循文档第 8 节，
// 避免把 recommend 的 pool_token 带到 hot/latest/following）
// 访客默认 hot tab；登录用户默认 recommend（原行为）
const activeTab = ref(auth.isAuthenticated() ? 'recommend' : 'hot')
const posts = ref([])
// 初始为 true：onMounted 的首次 fetchFeed 完成前即视为加载中，
// 避免首帧渲染出空态闪一下再切骨架屏
const loading = ref(true)
const isAppending = ref(false) // true=追加翻页（底部加载条 + NSpin）；false=首屏/切 tab（骨架屏）
const hasMore = ref(false)
const poolToken = ref('')   // 仅 recommend：候选池版本 token，翻页原样回传
// 仅 recommend：已消费的候选池条数（每页 +size）。私密圈 / 失效帖子在回填时被过滤（后端 #170），
// 单页可能不足 size 条，不能用已展示条数当 offset，否则会重复取到同一段池
let poolOffset = 0
const searchAfter = ref('') // 仅 hot/latest/following：游标原样透传

// 内容区当前视图：作为 Transition 的 key 一部分，视图切换（含切 tab）时触发平滑过渡
const feedView = computed(() => {
  if (loading.value && !isAppending.value) return 'loading'
  return posts.value.length === 0 ? 'empty' : 'list'
})

// 无限滚动
const sentinel = ref(null)
let observer = null

// 首屏/切 tab 的拉取世代：每次自增，用于丢弃切换竞态期间的过期响应
// （在飞请求未真正 abort，但其响应到达时若世代已变则忽略）
let fetchGen = 0

// 后端 snake_case → 组件 camelCase（PostList/PostCard 期望的字段）
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
    postTime: isNaN(ts) ? Date.now() : ts, // NTime 接受 number|Date
    viewCount: p.view_count || 0,
    likeCount: p.like_count || 0,
    commentCount: p.comment_count || 0,
    collectCount: p.collect_count || 0,
    // 当前列表项是否已点赞/已收藏（PostCard 暂未渲染高亮，预留后续接入）
    isLiked: !!p.is_liked,
    isCollected: !!p.is_collected,
    // 分享数 / 我是否分享过（后端 v0.9.0，PostCard 渲染分享按钮用；缺省时 null 表示不显示数字）
    shareCount: p.share_count ?? null,
    isShared: !!p.is_shared
  }
}

// 拉取信息流：append=true 表示追加下一页
const fetchFeed = async (append = false) => {
  // 追加翻页：已有请求在飞则跳过，避免重复追加
  if (append && loading.value) return
  // 首屏/切 tab：自增世代，使之前在飞的请求响应作废（切换竞态保护）
  if (!append) fetchGen++
  const gen = fetchGen

  loading.value = true
  isAppending.value = append
  try {
    const params = { tab: activeTab.value, size: PAGE_SIZE }
    if (activeTab.value === 'recommend') {
      // 候选池翻页：首页不传 offset/pool_token；翻页回传 pool_token + offset=已消费的池条数
      if (append && poolToken.value) {
        params.offset = poolOffset
        params.pool_token = poolToken.value
      }
    } else {
      // 游标翻页：search_after 原样透传（axios 自动 URL-encode）
      if (append && searchAfter.value) {
        params.search_after = searchAfter.value
      }
    }

    const res = await getHomeFeed(params)
    // 切 tab 竞态：期间又发起了新的首屏拉取，丢弃本次过期响应
    if (gen !== fetchGen) return
    const data = res.data || {}
    const list = (data.posts || []).map(transformPost)

    // recommend 池已重建：服务端回了 offset=0 的第 1 页，需重置本地列表到第 1 页
    if (data.pool_refreshed) {
      posts.value = list
      message.info(t('feed.refreshed'))
    } else {
      posts.value = append ? [...posts.value, ...list] : list
    }

    // 更新游标与是否还有更多
    if (activeTab.value === 'recommend') {
      poolToken.value = data.pool_token || ''
      // 首页 / 池已重建（服务端回 offset=0 第 1 页）后从 size 起算，翻页累加 size
      poolOffset = (params.offset !== undefined && !data.pool_refreshed ? poolOffset : 0) + PAGE_SIZE
      hasMore.value = !!data.has_more
    } else {
      searchAfter.value = data.search_after || ''
      // has_more=false 时不返回 search_after；二者同时判断更稳妥
      hasMore.value = !!data.has_more && !!searchAfter.value
    }
  } catch (error) {
    // 过期请求的错误不再提示
    if (gen !== fetchGen) return
    console.error('获取首页信息流失败:', error)
    message.error(error.message || t('feed.loadFailed'))
  } finally {
    // 仅当前世代才复位 loading/isAppending，避免过期请求把二者提前置 false
    if (gen === fetchGen) {
      loading.value = false
      isAppending.value = false
    }
  }
}

// 切 tab：清空本地列表 + 游标，用对应 tab 首页参数重新拉
const handleTabChange = (tab) => {
  // 访客访问 recommend/following tab：拦截并弹登录引导，保持在当前可读 tab
  if (!auth.isAuthenticated() && RESTRICTED_TABS.includes(tab)) {
    message.info(t('feed.loginRequiredTab'))
    requireLogin()
    // NTabs 已切换 DOM，需回滚到当前实际可读的 tab
    nextTick(() => { activeTab.value = isLoggedIn.value ? 'recommend' : 'hot' })
    return
  }
  activeTab.value = tab
  posts.value = []
  poolToken.value = ''
  searchAfter.value = ''
  hasMore.value = false
  if (observer) {
    observer.disconnect()
    observer = null
  }
  fetchFeed(false)
}

// 无限滚动：sentinel 进入视口时加载下一页（提前 200px 触发，体验更顺滑）
const setupObserver = () => {
  if (observer) {
    observer.disconnect()
    observer = null
  }
  if (!sentinel.value || !hasMore.value || loading.value) return

  observer = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting && hasMore.value && !loading.value) {
        fetchFeed(true)
      }
    },
    { rootMargin: '200px' }
  )
  observer.observe(sentinel.value)
}

watch([sentinel, hasMore, loading], setupObserver)

onMounted(() => {
  // 首页访客可读：activeTab 已在初始化时按登录态选定（访客=hot，登录=recommend）
  // recommend/following tab 的访客拦截在 handleTabChange 中处理
  fetchFeed(false)
})

onBeforeUnmount(() => {
  if (observer) observer.disconnect()
})

const handleLogout = async () => {
  try {
    // 调用后端登出接口
    await request.post('/auth/logout')

    // 清除本地 token
    auth.clearToken()
    message.success(t('common.logoutSuccess'))
    router.push('/')
  } catch (error) {
    // 即使接口调用失败，也清除本地 token
    auth.clearToken()
    message.warning(t('common.logout'))
    router.push('/')
  }
}
</script>

<style scoped>
.home-page {
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
}

.home-container {
  width: 100%;
  margin: 0 auto;
}

.content-header {
  margin-bottom: 24px;
}

/* 信息流：加载 / 空态 / 尾标 / 哨兵 */
.feed-body {
  /* 与上方 tab 栏拉开间距，避免首张卡片紧贴切换条 */
  margin-top: 16px;
  /* 切 tab 后列表清空、等待新数据期间保持最小高度，避免页面塌陷跳动 */
  min-height: 400px;
}

.feed-loading-more,
.feed-no-more {
  text-align: center;
  color: var(--text-secondary);
  font-size: 0.875rem;
  padding: 16px 0;
}

.feed-loading-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}

.feed-empty {
  text-align: center;
  color: var(--text-secondary);
  font-size: 0.95rem;
  padding: 64px 24px;
  line-height: 1.6;
}

.feed-sentinel {
  height: 1px;
}

/* 切 tab / 视图切换过渡：快速淡出 + 轻微上浮淡入，避免横向滑切的重排闪烁 */
.feed-switch-enter-active,
.feed-switch-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}

.feed-switch-enter-from {
  opacity: 0;
  transform: translateY(8px);
}

.feed-switch-leave-to {
  opacity: 0;
}

:deep(.n-card) {
  background: transparent !important;
}

:deep(.n-divider) {
  border-color: var(--glass-border) !important;
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

  .right-sidebar {
    display: none;
  }
}
</style>
