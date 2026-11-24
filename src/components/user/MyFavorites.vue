<template>
  <div class="my-favorites-tab">
    <div class="tab-header">
      <h2 class="tab-title">{{ t('user.myFavorites') }}</h2>
      <NInput
        v-model:value="searchKey"
        :placeholder="t('common.searchPosts')"
        clearable
        round
        class="tab-search"
        @keyup.enter="handleSearch"
        @clear="handleSearchClear">
        <template #prefix>
          <NIcon>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8"></circle>
              <path d="m21 21-4.35-4.35"></path>
            </svg>
          </NIcon>
        </template>
      </NInput>
      <NButton round @click="handleSearch">
        <template #icon>
          <NIcon>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8"></circle>
              <path d="m21 21-4.35-4.35"></path>
            </svg>
          </NIcon>
        </template>
        {{ t('common.search') }}
      </NButton>
    </div>

    <NSpin :show="loading && !isAppending">
      <div class="posts-list" :class="{ 'posts-list--loading': loading && !posts.length }">
        <div v-if="!loading && posts.length === 0" class="empty-state">
          <NIcon size="64" :depth="3">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
              <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"></path>
            </svg>
          </NIcon>
          <p class="empty-text">{{ t('post.noFavorites') }}</p>
        </div>
        <div v-else class="post-cards" :class="{ 'is-mobile': mobileShell }">
          <template v-for="post in posts" :key="post.id">
            <!-- 移动端外框：紧凑通栏列表项；桌面保持原卡片 -->
            <MobilePostItem
              v-if="mobileShell"
              :post-id="post.id"
              :title="post.title"
              :content="post.content"
              :images="post.images"
              :primary-name="post.authorName"
              :primary-avatar="post.authorAvatar"
              :secondary-name="post.circleName"
              :time-text="post.timeText"
              :pinned="!!post.isPinned"
              :essence="!!post.isEssence"
              :view-count="post.views"
              :like-count="post.likes"
              :comment-count="post.comments"
              :share-count="post.shareCount"
              :status="post.status"
              shareable
              flush />
          <NCard
            v-else
            class="post-card"
            :bordered="false"
            hoverable>
            <!-- 整卡封面链接（stretched-link）：铺满卡片承担帖子跳转，
                 右键/中键可新标签页打开；轮播抬 z-index 各自响应 -->
            <SmartLink class="post-cover-link" :to="`/post/${post.id}`" :aria-label="post.title" />
            <div class="post-header">
              <div class="post-author">
                <NAvatar
                  :size="40"
                  :src="post.authorAvatar"
                  round/>
                <div class="author-info">
                  <span class="author-name">{{ post.authorName }}</span>
                  <span class="post-time">{{ post.timeText }}</span>
                </div>
              </div>
            </div>
            <div class="post-title-row">
              <h3 class="post-title">{{ post.title }}</h3>
              <div v-if="post.isPinned || post.isEssence" class="post-badges">
                <NTag v-if="post.isPinned" size="tiny" round type="warning">{{ t('post.badges.pinned') }}</NTag>
                <NTag v-if="post.isEssence" size="tiny" round type="success">{{ t('post.badges.essence') }}</NTag>
              </div>
            </div>
            <p class="post-content">{{ post.content }}</p>
            <!-- 图片轮播：与帖子列表卡片统一使用 ImageCarousel -->
            <div v-if="post.images && post.images.length" class="post-carousel" @click.stop>
              <ImageCarousel :images="post.images" :parent-width="900" />
            </div>
            <div class="post-footer">
              <div class="post-tags">
                <span v-if="post.circleName" class="circle-tag">{{ post.circleName }}</span>
              </div>
              <div class="post-stats">
                <span class="stat">
                  <NIcon>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
                    </svg>
                  </NIcon>
                  {{ formatNumber(post.comments) }}
                </span>
                <span class="stat">
                  <NIcon>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
                    </svg>
                  </NIcon>
                  {{ formatNumber(post.likes) }}
                </span>
                <span class="stat">
                  <NIcon>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                      <circle cx="12" cy="12" r="3"></circle>
                    </svg>
                  </NIcon>
                  {{ formatNumber(post.views) }}
                </span>
                <span class="stat">
                  <NIcon>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                      <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"></path>
                    </svg>
                  </NIcon>
                  {{ formatNumber(post.collects) }}
                </span>
                <ShareButton
                  v-if="post.status === 1"
                  :post-id="post.id"
                  :title="post.title"
                  :share-count="post.shareCount"
                />
              </div>
            </div>
          </NCard>
          </template>
        </div>

        <!-- 无限滚动哨兵 -->
        <div ref="sentinel" class="scroll-sentinel"></div>
        <div v-if="posts.length" class="list-footer">
          <span v-if="loading" class="footer-text">{{ t('common.loading') }}</span>
          <span v-else-if="!hasMore" class="footer-text">{{ t('common.noMore') }}</span>
        </div>
      </div>
    </NSpin>
  </div>
</template>

<script setup>
import { ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { NCard, NAvatar, NIcon, NTag, NInput, NButton, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { getCollectedPosts } from '@/api/collect'
import { useFormatTime, useFormatNumber } from '@/utils/i18n'
import ImageCarousel from '@/components/common/ImageCarousel.vue'
import ShareButton from '@/components/post/ShareButton.vue'
import MobilePostItem from '@/components/post/MobilePostItem.vue'
import { useMobileShell } from '@/composables/useAppShell'
import SmartLink from '@/components/common/SmartLink.vue'

const message = useMessage()
const { t } = useI18n()
const { formatTime } = useFormatTime()
const { formatNumber } = useFormatNumber()
const mobileShell = useMobileShell()

const props = defineProps({
  // 当前 Tab 是否激活：离开时清空关键字搜索，切回即为干净的全量列表
  active: { type: Boolean, default: false }
})

const emit = defineEmits(['total-change'])

const searchKey = ref('')
const loading = ref(false)
const isAppending = ref(false) // true=追加翻页（走底部 loading 文案）；false=首次/搜索/刷新（走 NSpin 遮罩）

// 列表与分页状态（/collect/posts 采用 keyset 游标分页）
const PAGE_SIZE = 10
const posts = ref([])
const searchAfter = ref('') // 游标原样字符串，首页为空串；原样透传给下一页
const hasMore = ref(false)
const sentinel = ref(null)
let observer = null

// 后端 snake_case → 组件 camelCase（结构同帖子列表接口）
const transformPost = (p) => ({
  id: p.id,
  title: p.title || t('post.title'),
  content: p.summary || p.content || '',
  authorName: p.author_name || '',
  authorAvatar: p.author_avatar || '',
  circleName: p.circle_name || '',
  images: p.images || [],
  isPinned: p.is_pinned,
  isEssence: p.is_essence,
  timeText: formatTime(p.create_time || ''),
  comments: p.comment_count || 0,
  likes: p.like_count || 0,
  views: p.view_count || 0,
  shareCount: p.share_count ?? null,
  status: p.status ?? 1,
  collects: p.collect_count || 0
})

// 拉取收藏帖子：append=true 表示追加下一页
const fetchPosts = async (append = false) => {
  if (loading.value) return
  loading.value = true
  isAppending.value = append
  try {
    const params = { size: PAGE_SIZE }
    if (searchKey.value) params.keyword = searchKey.value
    if (append && searchAfter.value) {
      // 游标原样透传，不解析/不重新序列化（axios 会自动 URL-encode）
      params.search_after = searchAfter.value
    }

    const res = await getCollectedPosts(params)
    const data = res.data || {}
    const list = (data.posts || []).map(transformPost)
    posts.value = append ? [...posts.value, ...list] : list

    // search_after 为空字符串表示已到末页；否则原样保存供下一页透传
    searchAfter.value = data.search_after || ''
    hasMore.value = !!data.search_after

    // 仅在「非追加 + 无关键字」时上报真实总数，避免搜索结果数污染 Tab 徽标
    if (!append && !searchKey.value) {
      emit('total-change', data.total || 0)
    }
  } catch (error) {
    console.error('获取收藏列表失败:', error)
    message.error(error.message || t('common.operationFailed'))
  } finally {
    loading.value = false
  }
}

// 关键字搜索（按钮 / 回车主动触发，服务端匹配）
const handleSearch = () => {
  searchAfter.value = ''
  hasMore.value = false
  fetchPosts(false)
}

const handleSearchClear = () => {
  searchAfter.value = ''
  hasMore.value = false
  fetchPosts(false)
}

// 重置列表并从首页重新拉取（离开 Tab 清搜索时使用）
const resetAndFetch = () => {
  searchKey.value = ''
  searchAfter.value = ''
  hasMore.value = false
  posts.value = []
  fetchPosts(false)
}

// 无限滚动：sentinel 进入视口时加载下一页
const setupObserver = () => {
  if (observer) {
    observer.disconnect()
    observer = null
  }
  if (!sentinel.value || !hasMore.value || loading.value) return

  observer = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting && hasMore.value && !loading.value) {
        fetchPosts(true)
      }
    },
    { rootMargin: '200px' } // 提前 200px 触发，体验更顺滑
  )
  observer.observe(sentinel.value)
}

watch([sentinel, hasMore, loading], setupObserver)

// 离开本 Tab：若存在关键字搜索，清空并恢复全量列表（切回即为干净状态）
watch(() => props.active, (next, prev) => {
  if (prev && !next && searchKey.value) resetAndFetch()
})

onMounted(() => {
  fetchPosts(false)
})

onBeforeUnmount(() => {
  if (observer) observer.disconnect()
})
</script>

<style scoped>
.my-favorites-tab {
  padding: 8px 0;
}

.tab-header {
  display: flex;
  align-items: center;
  margin-bottom: 24px;
  flex-wrap: wrap;
  gap: 16px;
}

.tab-title {
  font-size: 1.5rem;
  font-weight: 600;
  margin-right: 3dvw;
  color: rgba(255, 255, 255, 0.95);
}

.posts-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* 加载占位：列表为空时撑高容器，避免 NSpin 动画被裁剪、页面收缩跳动 */
.posts-list--loading {
  min-height: 320px;
}

.post-cards {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* 移动端：通栏列表项自带分隔线，去掉卡片间距 */
.post-cards.is-mobile {
  gap: 0;
}

.post-card {
  position: relative;
  background: rgba(255, 255, 255, 0.02) !important;
  border: 1px solid rgba(255, 255, 255, 0.05) !important;
  border-radius: 16px !important;
  overflow: hidden;
  transition: all 0.3s ease;
  cursor: pointer;
}

@media (hover: hover) {
  .post-card:hover {
    background: rgba(255, 255, 255, 0.04) !important;
    border-color: rgba(255, 255, 255, 0.08) !important;
    transform: translateY(-2px);
  }
}

/* 整卡封面链接（stretched-link）：铺满卡片让浏览器在任意位置识别出帖子链接 */
.post-cover-link {
  position: absolute;
  inset: 0;
  z-index: 1;
}

.post-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 12px;
}

.post-author {
  display: flex;
  align-items: center;
  gap: 12px;
}

.author-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.author-name {
  font-size: 0.95rem;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.9);
}

.post-time {
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.5);
}

.post-title-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 0 0 8px 0;
}

.post-title {
  font-size: 1.1rem;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.95);
  margin: 0;
}

.post-badges {
  display: flex;
  gap: 6px;
  flex-shrink: 0;
}

.post-content {
  font-size: 0.95rem;
  color: rgba(255, 255, 255, 0.7);
  line-height: 1.6;
  margin: 0 0 12px 0;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

/* 图片轮播：与帖子列表卡片保持一致；抬层盖住封面链接 */
.post-carousel {
  position: relative;
  z-index: 2;
  margin-bottom: 12px;
}

.post-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.05);
}

.post-tags {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.circle-tag {
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.55);
  background: rgba(255, 255, 255, 0.05);
  padding: 2px 10px;
  border-radius: 10px;
}

.post-stats {
  display: flex;
  gap: 16px;
}

.post-stats .stat {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 0.875rem;
  color: rgba(255, 255, 255, 0.6);
}

.scroll-sentinel {
  height: 1px;
  width: 100%;
}

.list-footer {
  display: flex;
  justify-content: center;
  padding: 8px 0;
}

.footer-text {
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.4);
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  text-align: center;
}

.empty-text {
  font-size: 1.1rem;
  color: rgba(255, 255, 255, 0.6);
  margin: 16px 0 4px 0;
}

/* 响应式 */
@media (max-width: 768px) {
  .tab-header {
    flex-direction: column;
    align-items: stretch;
  }
}

/* 搜索框：桌面定宽，移动端占满整行（tab-header 在 768 以下纵向排列） */
.tab-search {
  width: 280px;
}

@media (max-width: 768px) {
  .tab-search {
    width: 100%;
  }
}

/* 移动端：标题与 Tab 名重复，隐藏；搜索框与按钮同一行。
   所在页面（我的 / 用户主页）在移动端左右通栏，这里自留 12px */
@media (max-width: 768px) {
  .tab-header {
    flex-direction: row;
    flex-wrap: nowrap;
    gap: 8px;
    margin-bottom: 12px;
    padding: 0 12px;
  }

  .tab-title {
    display: none;
  }

  .tab-search {
    flex: 1;
    min-width: 0;
  }
}
</style>
