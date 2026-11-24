<template>
  <div class="search-page">
    <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell） -->
    <AppShell v-model:offset="offset" />

    <!-- 主内容区域 -->
    <div class="main-content" :style="contentStyle">
      <div class="search-container">
          <!-- 移动端：顶栏只有搜索图标，搜索输入放在页内（无联想下拉，见 docs/mobile-adaptation-plan.md 2.2） -->
          <form v-if="mobileShell" class="mobile-search" action="" @submit.prevent="submitMobileSearch">
            <NInput
              ref="mobileSearchRef"
              v-model:value="mobileKeyword"
              round
              clearable
              size="large"
              :placeholder="t('common.searchPlaceholder')"
              :input-props="{ type: 'search', enterkeyhint: 'search' }"
            >
              <template #prefix>
                <NIcon><SearchIcon /></NIcon>
              </template>
            </NInput>
          </form>
          <div v-if="!mobileShell || keyword" class="search-info">
            <h2 class="search-title">{{ t('nav.searchResults') }}</h2>
            <p class="search-keyword">
              {{ t('nav.keyword') }}: {{ keyword }}
              <!-- <span v-if="circleId" class="circle-search-info"> | 圈子: {{ circleName }}</span> -->
            </p>
          </div>
          <NDivider v-if="!mobileShell || keyword"/>

        <!-- 搜索类型 Tabs -->
        <NTabs v-if="!mobileShell || keyword" animated v-model:value="activeTab" @update:value="handleTabChange">
          <NTabPane name="post" :tab="t('post.post')">
            <PostList
              v-if="posts.length > 0"
              :posts="posts"
            />
            <div v-else-if="!loadingPosts" class="tab-placeholder">
              <NIcon size="48" :color="'rgba(255,255,255,0.3)'">
                <FileTextIcon />
              </NIcon>
              <p>{{ t('post.noPostResults') }}</p>
            </div>
            <div v-if="loadingPosts" class="loading-placeholder">
              <NSpin size="medium" />
              <p>{{ t('post.searching') }}</p>
            </div>
            <div ref="postsSentinel" class="sentinel"></div>
          </NTabPane>
          <NTabPane name="circle" :tab="t('circle.interestCircle')">
            <CircleList
              :circles="circles"
              :loading="loading"
              :has-more="hasMore"
              @load-more="loadMore"
            />
          </NTabPane>
          <NTabPane name="user" :tab="t('user.user')">
            <UserList
              :users="users"
              :loading="loadingUsers"
              :has-more="hasMoreUsers"
              @load-more="loadMoreUsers"
            />
          </NTabPane>
        </NTabs>
      </div>
    </div>

    <!-- 右侧信息栏 -->
    <!-- <RightSidebar /> -->
  </div>
</template>

<script setup>
import { ref, onMounted, onBeforeUnmount, watch, inject } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NTabs,NDivider, NTabPane, NIcon, NSpin, NInput, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { FileText as FileTextIcon, Search as SearchIcon } from '@vicons/tabler'
import AppShell from '@/components/layout/AppShell.vue'
import { useAppShell } from '@/composables/useAppShell'
import CircleList from '@/components/circle/CircleList.vue'
import UserList from '@/components/user/UserList.vue'
import { searchCircles, getCircleDetail } from '@/api/circle'
import { searchPosts } from '@/api/post'
import { searchUsers } from '@/api/user'
import PostList from '@/components/post/PostList.vue'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const { t } = useI18n()
// 侧栏宽度与内容区偏移（移动端外框下不偏移），见 composables/useAppShell.js
const { offset, mobileShell, contentStyle } = useAppShell()

// 移动端页内搜索框
const mobileKeyword = ref(route.query.q || '')
const mobileSearchRef = ref(null)
const submitMobileSearch = () => {
  const q = mobileKeyword.value.trim()
  if (!q) return
  // 收起软键盘
  mobileSearchRef.value?.blur()
  router.replace({ path: '/search', query: { q, tab: activeTab.value } })
}

// 注入圈子搜索状态
const circleSearchState = inject('circleSearchState', ref({ id: null, name: '', avatarUrl: '' }))

// 搜索关键词
const keyword = ref('')
// 圈子ID（圈内搜索）
const circleId = ref(null)
const circleName = ref('')

// 当前激活的 tab
const activeTab = ref('post')

// 兴趣圈列表数据
const circles = ref([])
const loading = ref(false)
const hasMore = ref(false)
const searchAfter = ref(null)
const pageSize = 20

// 帖子列表数据
const posts = ref([])
const loadingPosts = ref(false)
const hasMorePosts = ref(false)
const searchAfterPosts = ref(null)

// 用户列表数据
const users = ref([])
const loadingUsers = ref(false)
const hasMoreUsers = ref(false)
const searchAfterUsers = ref(null)

// 帖子无限滚动相关
const postsSentinel = ref(null)
const postsObserver = ref(null)

// 初始化
onMounted(async () => {
  // 搜索页访客可读：/post/list /circle/list /user/search 已开放 anonymous

  // 获取搜索关键词
  keyword.value = route.query.q || ''
  if (!keyword.value) {
    // 移动端从顶栏搜索图标进入时没有关键词：停留本页，聚焦页内搜索框
    if (mobileShell.value) {
      mobileSearchRef.value?.focus()
      return
    }
    message.warning(t('search.emptyKeyword'))
    router.push('/home')
    return
  }

  // 获取初始搜索类型 tab
  if (route.query.tab && ['post', 'circle', 'user'].includes(route.query.tab)) {
    activeTab.value = route.query.tab
  }

  // 获取圈子ID（如果有）
  circleId.value = route.query.circle_id || null

  // 如果有圈子ID，获取圈子信息
  if (circleId.value) {
    // 优先使用注入的圈子搜索状态
    if (circleSearchState.value.id === circleId.value && circleSearchState.value.name) {
      circleName.value = circleSearchState.value.name
    } else {
      // 如果注入的状态不匹配，从API获取
      try {
        const response = await getCircleDetail(circleId.value)
        if (response.data) {
          circleName.value = response.data.name
        }
      } catch (error) {
        console.error('获取圈子信息失败:', error)
      }
    }
  }

  // 根据当前 tab 执行对应的搜索
  if (activeTab.value === 'post') {
    searchPostsData()
  } else if (activeTab.value === 'circle') {
    searchCirclesData()
  } else if (activeTab.value === 'user') {
    searchUsersData()
  }
})

// 监听路由变化
watch(() => route.query, async (newQuery) => {
  const newKeyword = newQuery.q
  const newCircleId = newQuery.circle_id || null
  const newTab = newQuery.tab

  // 同步 tab 参数
  if (newTab && ['post', 'circle', 'user'].includes(newTab)) {
    activeTab.value = newTab
  }

  if ((newKeyword && newKeyword !== keyword.value) || newCircleId !== circleId.value) {
    // newQuery.q 可能为 undefined（仅 circle_id 变化时），与 onMounted 一致归一为空串
    keyword.value = newKeyword || ''
    circleId.value = newCircleId

    // 如果有新的圈子ID，获取圈子信息
    if (circleId.value) {
      // 优先使用注入的圈子搜索状态
      if (circleSearchState.value.id === circleId.value && circleSearchState.value.name) {
        circleName.value = circleSearchState.value.name
      } else {
        // 如果注入的状态不匹配，从API获取
        try {
          const response = await getCircleDetail(circleId.value)
          if (response.data) {
            circleName.value = response.data.name
          }
        } catch (error) {
          console.error('获取圈子信息失败:', error)
        }
      }
    } else {
      circleName.value = ''
    }

    resetSearch()
    // 根据当前tab执行对应的搜索
    if (activeTab.value === 'post') {
      searchPostsData()
    } else if (activeTab.value === 'circle') {
      searchCirclesData()
    } else if (activeTab.value === 'user') {
      searchUsersData()
    }
  }
})

// 切换 tab
const handleTabChange = (value) => {
  activeTab.value = value
  // 根据 tab 切换执行对应的搜索
  if (value === 'post') {
    if (posts.value.length === 0) {
      searchPostsData()
    }
  } else if (value === 'circle') {
    if (circles.value.length === 0) {
      searchCirclesData()
    }
  } else if (value === 'user') {
    if (users.value.length === 0) {
      searchUsersData()
    }
  }
}

// 重置搜索状态
const resetSearch = () => {
  circles.value = []
  searchAfter.value = null
  hasMore.value = false
  posts.value = []
  searchAfterPosts.value = null
  hasMorePosts.value = false
  users.value = []
  searchAfterUsers.value = null
  hasMoreUsers.value = false
}

// 转换帖子数据格式
const transformPostData = (apiPosts) => {
  return apiPosts.map(post => ({
    postId: post.id,
    circleId: post.circle_id || null,
    circleName: post.circle_name || t('search.unknownCircle'),
    circleAvatar: post.circle_avatar || '',
    circleColor: '#ec4899',
    userId: post.user_id || null,
    userName: post.author_name || t('user.anonymous'),
    userAvatar: post.author_avatar || '',
    userColor: '#a855f7',
    title: post.title || t('search.untitled'),
    content: post.summary || post.content || '',
    coverImage: post.first_image || '',
    images: post.images || [],
    postTime: post.create_time || '',
    viewCount: post.view_count || 0,
    likeCount: post.like_count || 0,
    commentCount: post.comment_count || 0,
    collectCount: post.collect_count || 0,
    // /post/list 只返回 share_count，不返回 is_shared（按未分享显示）
    shareCount: post.share_count ?? null
  }))
}

// 搜索帖子数据
const searchPostsData = async () => {
  if (loadingPosts.value) return

  loadingPosts.value = true
  try {
    const params = {
      keyword: keyword.value,
      size: pageSize
    }

    // 如果有圈子ID，添加到参数中
    if (circleId.value) {
      params.circle_id = circleId.value
    }

    if (searchAfterPosts.value) {
      params.search_after = JSON.stringify(searchAfterPosts.value)
    }

    const response = await searchPosts(params)

    if (response.code === 200 && response.data) {
      const newPosts = response.data.posts || []
      const transformedPosts = transformPostData(newPosts)

      if (searchAfterPosts.value) {
        // 加载更多，追加数据
        posts.value = [...posts.value, ...transformedPosts]
      } else {
        // 首次加载或新搜索
        posts.value = transformedPosts
      }

      // 更新 search_after
      if (response.data.search_after) {
        try {
          searchAfterPosts.value = JSON.parse(response.data.search_after)
          hasMorePosts.value = true
        } catch (e) {
          console.error('解析 search_after 失败:', e)
          hasMorePosts.value = false
        }
      } else {
        hasMorePosts.value = false
      }
    } else {
      // 响应信封的错误字段是 message（与 L416/L474 一致），优先展示服务端文案
      message.error(response.message || t('search.failed'))
    }
  } catch (error) {
    console.error('搜索帖子失败:', error)
    message.error(t('search.failedRetry'))
  } finally {
    loadingPosts.value = false
  }
}

// 加载更多帖子
const loadMorePosts = () => {
  if (!loadingPosts.value && hasMorePosts.value) {
    searchPostsData()
  }
}

// 设置帖子无限滚动观察器
const setupPostsObserver = () => {
  // 清理旧的观察器
  if (postsObserver.value) {
    postsObserver.value.disconnect()
  }

  // 只有在有更多数据且不在加载状态时才创建观察器
  if (!postsSentinel.value || !hasMorePosts.value || loadingPosts.value) {
    return
  }

  postsObserver.value = new IntersectionObserver(
    (entries) => {
      const entry = entries[0]
      if (entry.isIntersecting && hasMorePosts.value && !loadingPosts.value) {
        loadMorePosts()
      }
    },
    {
      rootMargin: '200px' // 提前200px开始加载
    }
  )

  postsObserver.value.observe(postsSentinel.value)
}

// 监听帖子的 sentinel、hasMorePosts 和 loadingPosts 变化
watch([postsSentinel, hasMorePosts, loadingPosts], () => {
  setupPostsObserver()
})

// 组件卸载时清理观察器
onBeforeUnmount(() => {
  if (postsObserver.value) {
    postsObserver.value.disconnect()
  }
})

// 搜索兴趣圈数据
const searchCirclesData = async () => {
  if (loading.value) return

  loading.value = true
  try {
    const params = {
      keyword: keyword.value,
      size: pageSize
    }

    if (searchAfter.value) {
      params.search_after = JSON.stringify(searchAfter.value)
    }

    const response = await searchCircles(params)

    if (response.code === 200 && response.data) {
      const newCircles = response.data.circles || []

      if (searchAfter.value) {
        // 加载更多，追加数据
        circles.value = [...circles.value, ...newCircles]
      } else {
        // 首次加载或新搜索
        circles.value = newCircles
      }

      // 更新 search_after
      if (response.data.search_after) {
        try {
          searchAfter.value = JSON.parse(response.data.search_after)
          hasMore.value = true
        } catch (e) {
          console.error('解析 search_after 失败:', e)
          hasMore.value = false
        }
      } else {
        hasMore.value = false
      }
    } else {
      message.error(response.message || t('search.failed'))
    }
  } catch (error) {
    console.error('搜索兴趣圈失败:', error)
    message.error(t('search.failedRetry'))
  } finally {
    loading.value = false
  }
}

// 加载更多
const loadMore = () => {
  if (!loading.value && hasMore.value) {
    searchCirclesData()
  }
}

// 搜索用户数据
const searchUsersData = async () => {
  if (loadingUsers.value) return

  loadingUsers.value = true
  try {
    const params = {
      keyword: keyword.value,
      size: pageSize
    }

    if (searchAfterUsers.value) {
      params.search_after = JSON.stringify(searchAfterUsers.value)
    }

    const response = await searchUsers(params)

    if (response.code === 200 && response.data) {
      const newUsers = response.data.data || []

      if (searchAfterUsers.value) {
        // 加载更多，追加数据
        users.value = [...users.value, ...newUsers]
      } else {
        // 首次加载或新搜索
        users.value = newUsers
      }

      // 更新 search_after
      if (response.data.search_after) {
        try {
          searchAfterUsers.value = JSON.parse(response.data.search_after)
          hasMoreUsers.value = true
        } catch (e) {
          console.error('解析 search_after 失败:', e)
          hasMoreUsers.value = false
        }
      } else {
        hasMoreUsers.value = false
      }
    } else {
      message.error(response.message || t('search.failed'))
    }
  } catch (error) {
    console.error('搜索用户失败:', error)
    message.error(t('search.failedRetry'))
  } finally {
    loadingUsers.value = false
  }
}

// 加载更多用户
const loadMoreUsers = () => {
  if (!loadingUsers.value && hasMoreUsers.value) {
    searchUsersData()
  }
}
</script>

<style scoped>
.search-page {
  min-height: 100vh;
  position: relative;
}

.main-content {
  margin-top: var(--header-height);
  padding: 24px;
  min-height: calc(100vh - var(--header-height));
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.search-container {
  width: 100%;
  max-width: 900px;
  margin: 0 auto;
}

.search-header {
  margin-bottom: 24px;
  padding: 20px 24px;
  background: rgba(30, 30, 46, 0.6);
  backdrop-filter: blur(20px);
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
}

.search-info {
  display: flex;
  align-items: center;
  gap: 16px;
}

.search-title {
  font-size: 1.5rem;
  font-weight: 700;
  background: rgba(103, 255, 181, 0.896);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
  margin: 0;
}

.search-keyword {
  color: rgba(255, 255, 255, 0.6);
  font-size: 0.95rem;
  margin: 0;
}

.circle-search-info {
  color: rgb(236, 72, 153);
  font-weight: 500;
}

.tab-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 80px 20px;
  color: rgba(255, 255, 255, 0.4);
  font-size: 1rem;
}

.tab-placeholder p {
  margin-top: 16px;
}

.loading-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  color: rgba(255, 255, 255, 0.6);
}

.loading-placeholder p {
  margin-top: 16px;
}

.load-more-container {
  display: flex;
  justify-content: center;
  padding: 20px 0;
}

/* 响应式 */
@media (max-width: 1200px) {
  .main-content {
    margin-right: 24px;
  }
}

@media (max-width: 768px) {
  .main-content {
    margin-left: 0;
    margin-right: 0;
    padding: 16px;
  }

  .search-info {
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
  }
}

/* NaiveUI 组件样式覆盖 */
:deep(.n-tabs) {
  --n-color-segment: rgba(255, 255, 255, 0.05) !important;
}

:deep(.n-tab-pane) {
  padding: 16px 0 0 0 !important;
}

.sentinel {
  width: 100%;
  height: 1px;
}

/* 移动端页内搜索框 */
.mobile-search {
  margin-bottom: 12px;
}

.mobile-search :deep(.n-input__input-el) {
  /* ≥ 16px：iOS 聚焦时不自动缩放页面 */
  font-size: 16px;
}
</style>
