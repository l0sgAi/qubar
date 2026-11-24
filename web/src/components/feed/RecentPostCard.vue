<template>
  <SmartLink class="recent-post-card" :to="postId ? `/post/${postId}` : null">
    <!-- 帖子信息 -->
    <div class="post-info">
      <!-- 兴趣圈标签（头像 + 圈名） -->
      <div class="circle-tag">
        <div class="circle-avatar">
          <img v-if="circleAvatar" :src="circleAvatar" :alt="circleName" class="circle-avatar-img" />
          <div v-else class="circle-avatar-fallback">{{ circleName.charAt(0) }}</div>
        </div>
        <span class="circle-name">{{ circleName }}</span>
      </div>

      <!-- 帖子标题 -->
      <h4 class="post-title">{{ title }}</h4>

      <!-- 帖子摘要 -->
      <p class="post-summary">{{ summary }}</p>

      <!-- 帖子元数据 -->
      <div class="post-meta">
        <div class="meta-item">
          <svg class="meta-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
          </svg>
          <span class="meta-text">{{ likeCount }}</span>
        </div>

        <div class="meta-item">
          <svg class="meta-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"></path>
          </svg>
          <span class="meta-text">{{ commentCount }}</span>
        </div>

        <ShareCount :post-id="postId" :share-count="shareCount" :is-shared="isShared" />

        <div class="meta-item time">
          <svg class="meta-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <circle cx="12" cy="12" r="10"></circle>
            <polyline points="12 6 12 12 16 14"></polyline>
          </svg>
          <span class="meta-text"><NTime :time="postTime" :to="Date.now()" type="relative" /></span>
        </div>
      </div>
    </div>

    <!-- 缩略图（无图帖子不渲染该区域，正文占满整行宽度；显示在列表项右侧） -->
    <div v-if="thumbnail" class="post-thumbnail">
      <img :src="thumbnail" :alt="title" class="thumbnail-image" />
    </div>
  </SmartLink>
</template>

<script setup>
import { computed } from 'vue'
import { NTime } from 'naive-ui'
import ShareCount from '@/components/post/ShareCount.vue'
import SmartLink from '@/components/common/SmartLink.vue'

const props = defineProps({
  postId: {
    type: String,
    required: true
  },
  circleName: {
    type: String,
    required: true
  },
  circleAvatar: {
    type: String,
    default: ''
  },
  title: {
    type: String,
    required: true
  },
  content: {
    type: String,
    required: true
  },
  images: {
    type: Array,
    default: () => []
  },
  postTime: {
    type: [Date, String, Number],
    required: true
  },
  shareCount: { type: Number, default: null },
  isShared: { type: Boolean, default: false },
  likeCount: {
    type: Number,
    default: 0
  },
  commentCount: {
    type: Number,
    default: 0
  }
})

// 获取第一张图片作为缩略图
const thumbnail = computed(() => {
  return props.images && props.images.length > 0 ? props.images[0] : null
})

// 生成摘要（截取前160个字符）
const summary = computed(() => {
  return props.content.length > 160 ? props.content.substring(0, 160) + '...' : props.content
})
</script>

<style scoped>
.recent-post-card {
  display: flex;
  gap: 12px;
  padding: 12px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 12px;
  cursor: pointer;
  transition: all 0.3s ease;
  margin-bottom: 12px;
}

@media (hover: hover) {
  .recent-post-card:hover {
    background: rgba(255, 255, 255, 0.05);
    border-color: rgba(255, 255, 255, 0.12);
    transform: translateX(4px);
  }
}

/* 缩略图 */
.post-thumbnail {
  flex-shrink: 0;
  width: 80px;
  height: 80px;
  border-radius: 8px;
  overflow: hidden;
  background: rgba(255, 255, 255, 0.05);
}

.thumbnail-image {
  width: 100%;
  height: 100%;
  object-fit: contain;
  transition: transform 0.3s ease;
}

@media (hover: hover) {
  .recent-post-card:hover .thumbnail-image {
    transform: scale(1.05);
  }
}

/* 帖子信息 */
.post-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

/* 兴趣圈标签 */
.circle-tag {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 0.75rem;
  font-weight: 600;
  color: var(--theme-color);
  opacity: 0.9;
}

.circle-avatar {
  flex-shrink: 0;
  width: 18px;
  height: 18px;
  border-radius: 50%;
  overflow: hidden;
  background: rgba(102, 234, 194, 0.14);
}

.circle-avatar-img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.circle-avatar-fallback {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.65rem;
  font-weight: 700;
  color: #8af0d0;
  line-height: 1;
}

.circle-name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* 帖子标题 */
.post-title {
  font-size: 0.9rem;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.95);
  margin: 0;
  line-height: 1.3;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

/* 帖子摘要 */
.post-summary {
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.6);
  margin: 0;
  line-height: 1.4;
  overflow: hidden;
  text-overflow: ellipsis;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
}

/* 帖子元数据 */
.post-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  margin-top: auto;
}

.meta-item {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 0.75rem;
  color: rgba(255, 255, 255, 0.5);
}

.meta-item.time {
  margin-left: auto;
}

.meta-icon {
  width: 14px;
  height: 14px;
}

.meta-text {
  font-size: 0.75rem;
  color: rgba(255, 255, 255, 0.5);
}

/* NaiveUI 时间样式覆盖 */
:deep(.n-time) {
  font-size: 0.75rem !important;
  color: rgba(255, 255, 255, 0.5) !important;
}
</style>
