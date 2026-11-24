<template>
  <!-- 发现帖子方块（灵感墙用）：紧凑卡片，有图显示首图，无图显示文字 -->
  <SmartLink class="discover-post-tile" :to="post.postId ? `/post/${post.postId}` : null">
    <div v-if="coverImage" class="tile-banner">
      <img :src="coverImage" :alt="title" loading="lazy" />
    </div>
    <div class="tile-body">
      <h4 class="tile-title">{{ title }}</h4>
      <p v-if="content" class="tile-text">{{ content }}</p>
      <div class="tile-meta">
        <span class="meta-author">{{ userName }}</span>
        <span class="meta-dot">·</span>
        <span class="meta-circle">{{ circleName }}</span>
      </div>
      <div class="tile-stats">
        <span class="stat">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
          {{ formatNumber(viewCount) }}
        </span>
        <span class="stat">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"/></svg>
          {{ formatNumber(likeCount) }}
        </span>
        <span class="stat">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"/></svg>
          {{ formatNumber(commentCount) }}
        </span>
        <!-- 分享数（只读）：整块是帖子链接，<a> 里不能再放按钮，分享在卡片 / 详情页操作 -->
        <ShareCount
          class="stat"
          :post-id="post.postId"
          :share-count="post.shareCount"
          :is-shared="post.isShared"
        />
      </div>
    </div>
  </SmartLink>
</template>

<script setup>
import { computed } from 'vue'
import { useFormatNumber } from '@/utils/i18n'
import SmartLink from '@/components/common/SmartLink.vue'
import ShareCount from '@/components/post/ShareCount.vue'

const props = defineProps({
  // 已 transform 的 post 对象（camelCase，与 PostCard 同源）
  post: {
    type: Object,
    required: true
  }
})

const { formatNumber } = useFormatNumber()
const title = computed(() => props.post.title || '')
const content = computed(() => props.post.content || '')
const userName = computed(() => props.post.userName || '')
const circleName = computed(() => props.post.circleName || '')
const viewCount = computed(() => props.post.viewCount || 0)
const likeCount = computed(() => props.post.likeCount || 0)
const commentCount = computed(() => props.post.commentCount || 0)
// 封面图：多图取第一张，否则回退 coverImage
const coverImage = computed(() => {
  const imgs = props.post.images
  if (Array.isArray(imgs) && imgs.length > 0) return imgs[0]
  return props.post.coverImage || ''
})
</script>

<style scoped>
.discover-post-tile {
  display: flex;
  flex-direction: column;
  border-radius: 16px;
  overflow: hidden;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  cursor: pointer;
  height: 100%;
  box-sizing: border-box;
  transition: all 0.2s ease;
}

@media (hover: hover) {
  .discover-post-tile:hover {
    background: rgba(255, 255, 255, 0.06);
    border-color: rgba(96, 248, 187, 0.3);
    transform: translateY(-2px);
  }
}

.tile-banner {
  width: 100%;
  aspect-ratio: 16 / 9;
  overflow: hidden;
  background: rgba(255, 255, 255, 0.04);
}

.tile-banner img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.tile-body {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 12px 14px 14px;
  flex: 1;
}

.tile-title {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  line-height: 1.4;
  color: rgba(255, 255, 255, 0.95);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.tile-text {
  margin: 0;
  font-size: 12px;
  line-height: 1.5;
  color: rgba(255, 255, 255, 0.5);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.tile-meta {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-top: auto;
}

.meta-dot {
  margin: 0 5px;
  opacity: 0.4;
}

.meta-circle {
  color: #60F8BB;
}

.tile-stats {
  display: flex;
  gap: 14px;
  padding-top: 8px;
  border-top: 1px solid rgba(255, 255, 255, 0.06);
}

.stat {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
}

.stat svg {
  width: 14px;
  height: 14px;
}

</style>
