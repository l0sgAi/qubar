<template>
  <MobilePostItem
    v-if="mobileShell"
    :post-id="postId"
    :title="title"
    :content="content"
    :images="displayImages"
    :primary-name="showCircle ? circleName : userName"
    :primary-avatar="showCircle ? circleAvatar : userAvatar"
    :primary-to="showCircle ? (circleId ? `/circle/${circleId}` : null) : (userId ? `/user/${userId}` : null)"
    :secondary-name="showCircle ? userName : ''"
    :secondary-to="userId ? `/user/${userId}` : null"
    :time-text="formatTime(postTime)"
    :view-count="viewCount"
    :like-count="likeCount"
    :comment-count="commentCount"
    :share-count="shareCount"
    :is-shared="isShared"
    shareable
    more-menu
    :author-id="userId"
    :status="status"
  />
  <NCard v-else class="post-card" :bordered="false">
    <!-- 整卡封面链接（stretched-link）：铺满卡片承担帖子跳转，hover 任意位置显示
         /post/:id、右键/中键可新标签页打开；圈子/用户/轮播等交互区抬 z-index 各自响应 -->
    <SmartLink class="post-cover-link" :to="`/post/${postId}`" :aria-label="title" />
    <!-- 帖子头部 -->
    <div v-if="showCircle" class="post-header">
      <SmartLink class="circle-info" :to="circleId ? `/circle/${circleId}` : null">
        <NAvatar
          round
          :size="48"
          :src="circleAvatar"
        >
          <div v-if="circleAvatar===undefined || circleAvatar===''">{{ circleName.charAt(0) }}</div>
        </NAvatar>
        <span class="circle-name">{{ circleName }}</span>
      </SmartLink>
      <div class="header-spacer"></div>
      <SmartLink class="user-info" :to="userId ? `/user/${userId}` : null">
        <div class="user-meta">
          <div class="user-name">{{ userName }}</div>
          <div class="post-time">
            <NTime :time="postTime" format="yyyy-MM-dd HH:mm" />
          </div>
        </div>
      </SmartLink>
      <PostMoreMenu :post-id="postId" :author-id="userId" :status="status" />
    </div>
    <div v-else class="post-header">
      <SmartLink class="author-info" :to="userId ? `/user/${userId}` : null">
        <NAvatar round :size="40" :src="userAvatar">
          <div v-if="!userAvatar">{{ userName.charAt(0) }}</div>
        </NAvatar>
        <div class="user-meta">
          <div class="user-name">{{ userName }}</div>
          <div class="post-time">
            <NTime :time="postTime" format="yyyy-MM-dd HH:mm" />
          </div>
        </div>
      </SmartLink>
      <div class="header-spacer"></div>
      <PostMoreMenu :post-id="postId" :author-id="userId" :status="status" />
    </div>

    <!-- 帖子内容 -->
    <div class="post-content">
      <h3 class="post-title">{{ title }}</h3>
      <p v-if="content" ref="textRef" class="post-text">{{ content }}</p>
      <span v-if="content && isTruncated" class="view-detail">{{ t('post.viewDetail') }} ></span>
    </div>

    <!-- 图片轮播：封面图与多图统一展示 -->
    <div v-if="displayImages.length > 0" class="post-carousel" @click.stop>
      <ImageCarousel :images="displayImages" :parent-width="900" />
    </div>

    <!-- 搜索结果：统计信息（仅展示） -->
    <div class="post-stats">
      <div class="stat-item">
        <NIcon class="stat-icon" size="18">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
            <circle cx="12" cy="12" r="3"></circle>
          </svg>
        </NIcon>
        <span>{{ formatNumber(viewCount, STATS_COUNT_CAP) }}</span>
      </div>
      <div class="stat-item">
        <NIcon class="stat-icon" size="18">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"></path>
          </svg>
        </NIcon>
        <span>{{ formatNumber(commentCount, STATS_COUNT_CAP) }}</span>
      </div>
      <div class="stat-item">
          <NIcon class="stat-icon" size="18">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
            </svg>
          </NIcon>
        <span>{{ formatNumber(likeCount, STATS_COUNT_CAP) }}</span>
      </div>
      <div class="stat-item">
        <NIcon class="stat-icon" size="18">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"></path>
          </svg>
        </NIcon>
        <span>{{ formatNumber(collectCount, STATS_COUNT_CAP) }}</span>
      </div>
      <!-- 分享：只有已发布的帖子可分享（未发布的分享计数接口返回 404） -->
      <ShareButton
        v-if="status === 1"
        :post-id="postId"
        :title="title"
        :share-count="shareCount"
        :is-shared="isShared"
        :count-cap="STATS_COUNT_CAP"
      />
    </div>
  </NCard>
</template>

<script setup>
import { ref, onBeforeUnmount, watch, nextTick, computed } from 'vue'
import { NCard, NAvatar, NButton, NIcon, NTime, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import ImageCarousel from '@/components/common/ImageCarousel.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import MobilePostItem from '@/components/post/MobilePostItem.vue'
import PostMoreMenu from '@/components/post/PostMoreMenu.vue'
import ShareButton from '@/components/post/ShareButton.vue'
import { useMobileShell } from '@/composables/useAppShell'
import { useFormatNumber, useFormatTime } from '@/utils/i18n'

const { t } = useI18n()
const { formatNumber } = useFormatNumber()
const { formatTime } = useFormatTime()
// 移动端外框：渲染紧凑列表项（一屏多条）；桌面保持原卡片
const mobileShell = useMobileShell()

const STATS_COUNT_CAP = 100_000_000

const props = defineProps({
  postId: {
    type: String,
    required: true
  },
  circleId: {
    type: String,
    default: null
  },
  circleName: {
    type: String,
    required: true
  },
  circleAvatar: {
    type: String,
    default: ''
  },
  circleColor: {
    type: String,
    default: '#ec4899'
  },
  userId: {
    type: String,
    default: null
  },
  userName: {
    type: String,
    required: true
  },
  userAvatar: {
    type: String,
    default: ''
  },
  userColor: {
    type: String,
    default: '#a855f7'
  },
  title: {
    type: String,
    required: true
  },
  content: {
    type: String,
    default: ''
  },
  coverImage: {
    type: String,
    default: ''
  },
  images: {
    type: Array,
    default: () => []
  },
  postTime: {
    type: [Date, String, Number],
    required: true
  },
  viewCount: {
    type: Number,
    default: 0
  },
  likeCount: {
    type: Number,
    default: 0
  },
  commentCount: {
    type: Number,
    default: 0
  },
  collectCount: {
    type: Number,
    default: 0
  },
  // 分享数：null = 该列表接口没给（只显示分享图标）
  shareCount: {
    type: Number,
    default: null
  },
  // 当前用户是否分享过：只有首页 / 热门 / 发现 / 详情返回，缺省按未分享
  isShared: {
    type: Boolean,
    default: false
  },
  showCircle: {
    type: Boolean,
    default: true
  },
  // 帖子状态：信息流 / 搜索 / 圈子列表只返回已发布的帖子，未传按已发布处理（决定是否显示「举报」）
  status: {
    type: Number,
    default: 1
  }
})

const message = useMessage()

// 统一图片列表：有多图就用 images，否则回退到单张封面图。
// 这样封面图与非封面图帖子都用同一个轮播组件展示，摘要截断策略也保持一致。
const displayImages = computed(() => {
  if (props.images && props.images.length > 0) return props.images
  return props.coverImage ? [props.coverImage] : []
})

// 摘要按高度截断后，检测是否真的溢出（scrollHeight > clientHeight），
// 仅在溢出时显示「查看详情」提示。ResizeObserver 兜底宽度变化导致的重排。
const textRef = ref(null)
const isTruncated = ref(false)
let resizeObserver = null

const checkTruncation = () => {
  const el = textRef.value
  isTruncated.value = !!el && el.scrollHeight - el.clientHeight > 1
}

// 摘要节点随断点切换（移动端紧凑项 ↔ 桌面卡片）出现 / 消失，按节点挂载 / 卸载观察器
watch(textRef, (el) => {
  resizeObserver?.disconnect()
  resizeObserver = null
  if (!el) return
  nextTick(checkTruncation)
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(checkTruncation)
    resizeObserver.observe(el)
  }
}, { flush: 'post' })

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
})

watch(() => props.content, () => {
  nextTick(checkTruncation)
})

const handleComment = () => {
  message.info(t('common.featureInDevelopment'))
}
</script>

<style scoped>
.post-card {
  position: relative;
  margin-bottom: 20px;
  background: rgba(255, 255, 255, 0.03) !important;
  border: 1px solid rgba(255, 255, 255, 0.08) !important;
  border-radius: 16px !important;
  overflow: hidden;
  transition: all 0.3s ease;
}

@media (hover: hover) {
  .post-card:hover {
    background: rgba(255, 255, 255, 0.05) !important;
    border-color: rgba(255, 255, 255, 0.12) !important;
    transform: translateY(-2px);
    cursor: pointer;
  }
}

/* 整卡封面链接（stretched-link）：铺满卡片让浏览器在任意位置识别出帖子链接
   （hover 状态栏 URL / 右键新标签页）；内层交互区（圈子/用户/轮播）抬层优先响应 */
.post-cover-link {
  position: absolute;
  inset: 0;
  z-index: 1;
}

/* 帖子头部 */
.post-header {
  display: flex;
  padding: 8px;
  align-items: center;
  gap: 12px;
}

.circle-info {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: center;
  gap: 15px;
  padding: 4px 10px;
  flex-shrink: 0;
}

.circle-info :deep(.n-avatar) {
  color: white;
  font-weight: 700;
  font-size: 1.5rem;
}

.circle-name {
  font-size: 18px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.8);
}

@media (hover: hover) {
  .circle-info:hover .circle-name,
  .user-info:hover .user-name,
  .author-info:hover .user-name {
    color: rgba(255, 255, 255, 1);
  }
}

@media (hover: hover) {
  .circle-info:hover {
    cursor: pointer;
    background: rgba(255, 255, 255, 0.06);
    border-radius: 8px;
  }
}

.user-info {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 10px;
  border-radius: 8px;
}

@media (hover: hover) {
  .user-info:hover {
    cursor: pointer;
    background: rgba(255, 255, 255, 0.06);
  }
}

.header-spacer {
  flex: 1;
}

.author-info {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 4px 10px;
  border-radius: 8px;
}

.author-info :deep(.n-avatar) {
  color: white;
  font-weight: 700;
  font-size: 1.2rem;
}

@media (hover: hover) {
  .author-info:hover {
    cursor: pointer;
    background: rgba(255, 255, 255, 0.06);
  }
}

.user-meta {
  display: flex;
  flex-direction: column;
  gap: 2px;
  align-items: flex-start;
}

.user-name {
  font-size: 0.85rem;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.9);
}

.post-time {
  font-size: 0.7rem;
  color: rgba(255, 255, 255, 0.5);
}

/* 帖子内容 */
.post-content {
  margin-bottom: 16px;
}

.post-title {
  font-size: 1.1rem;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.95);
  margin-bottom: 8px;
  margin-left: 16px;
  line-height: 1.4;
}

.post-text {
  font-size: 0.95rem;
  margin-left: 16px;
  color: rgba(255, 255, 255, 0.8);
  line-height: 1.6;
  /* 保留摘要文本中的换行（\n），同时长行仍自动折行 */
  white-space: pre-wrap;
  word-break: break-word;
  /* 按高度截断：≈ 6 行（line-height 1.6 → 9.6em），超出裁掉。
     不用 -webkit-line-clamp，它强制 white-space:normal 会丢失换行。 */
  max-height: 28.8em;
  overflow: hidden;
}

/* 摘要溢出时的「查看详情」提示，项目主题蒂芙尼绿 #66eac2 */
.view-detail {
  display: inline-block;
  margin-left: 16px;
  margin-top: 6px;
  font-size: 0.85rem;
  font-weight: 500;
  color: #66eac2;
  opacity: 0.85;
  transition: opacity 0.2s ease;
}

@media (hover: hover) {
  .post-card:hover .view-detail {
    opacity: 1;
  }
}

/* 图片轮播（封面图/多图统一）：与摘要文本左对齐（16px）；
   抬层盖住封面链接，图片交互不触发整卡跳转 */
.post-carousel {
  position: relative;
  z-index: 2;
  margin-left: 16px;
  margin-bottom: 12px;
}

/* 搜索结果：统计信息 */
.post-stats {
  display: flex;
  gap: 24px;
  padding-top: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 6px;
  color: rgba(255, 255, 255, 0.6);
  font-size: 14px;
}

.stat-icon {
  color: rgba(255, 255, 255, 0.5);
}

/* 非搜索结果：操作按钮栏 */
.post-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-top: 16px;
  border-top: 1px solid rgba(255, 255, 255, 0.08);
}

:deep(.n-button) {
  color: rgba(255, 255, 255, 0.6) !important;
}

@media (hover: hover) {
  :deep(.n-button:hover) {
    color: rgba(255, 255, 255, 0.9) !important;
    background: rgba(255, 255, 255, 0.08) !important;
  }
}

:deep(.n-button.n-button--error-type) {
  color: #ec4899 !important;
}

@media (hover: hover) {
  :deep(.n-button.n-button--error-type:hover) {
    background: rgba(236, 72, 153, 0.15) !important;
  }
}

:deep(.n-button.n-button--warning-type) {
  color: #f59e0b !important;
}

@media (hover: hover) {
  :deep(.n-button.n-button--warning-type:hover) {
    background: rgba(245, 158, 11, 0.15) !important;
  }
}

/* 响应式 */
@media (max-width: 768px) {
  .post-image {
    aspect-ratio: 4 / 3;
  }

  .post-stats {
    flex-wrap: wrap;
    gap: 16px;
  }
}
</style>
