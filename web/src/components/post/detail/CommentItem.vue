<template>
  <div
    :id="anchor ? 'comment-' + comment.id : undefined"
    class="comment-item"
    :class="{ 'reply-item': isReply, 'comment-highlight': highlighted }"
  >
    <div class="comment-avatar">
      <NAvatar round :size="isReply ? 28 : 36" :src="comment.author_avatar || undefined">
        <div v-if="!comment.author_avatar">{{ comment.author_name?.charAt(0) }}</div>
      </NAvatar>
    </div>
    <div class="comment-body">
      <div class="comment-author-row">
        <SmartLink class="comment-author" :class="{ clickable: getUserId(comment) }" :to="userUrl(getUserId(comment))">{{ comment.author_name }}</SmartLink>
        <template v-if="replyToName">
          <span class="reply-arrow">{{ t('comment.actions.reply') }}</span>
          <SmartLink class="comment-author reply-to-name" :class="{ clickable: replyToUserId }" :to="userUrl(replyToUserId)">@{{ replyToName }}</SmartLink>
        </template>
        <span class="comment-time">{{ formatTime(comment.create_time) }}</span>
      </div>
      <ImageCarousel
        v-if="images.length"
        :images="images"
        :parent-width="isReply ? 670 : 800"
      />
      <div class="comment-content">
        <MentionPreview
          :content="comment.content"
          :mentions="comment.mentions"
          :language="language"
        />
      </div>
      <div class="comment-actions">
        <NButton text size="small" class="comment-action-btn" :class="{ 'liked-btn': comment.liked }" @click="emit('like', comment)">
          <template #icon>
            <NIcon size="16">
              <svg viewBox="0 0 24 24" :fill="comment.liked ? 'currentColor' : 'none'" stroke="currentColor" stroke-width="2">
                <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
              </svg>
            </NIcon>
          </template>
          {{ formatNumber(comment.like_count) }}
        </NButton>
        <NButton text size="small" class="comment-action-btn" @click="emit('reply', comment)">
          <template #icon>
            <NIcon size="16">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"></path>
              </svg>
            </NIcon>
          </template>
          {{ t('comment.actions.reply') }}
        </NButton>
      </div>

      <!-- 内联回复编辑器、查看回复、子回复区等由父组件按场景填充 -->
      <slot />
    </div>
  </div>
</template>

<script setup>
// 单条评论（根评论或回复）的展示：头像、作者行、配图、正文、点赞 / 回复按钮
import { computed } from 'vue'
import { NAvatar, NButton, NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import ImageCarousel from '@/components/common/ImageCarousel.vue'
import MentionPreview from '@/components/post/detail/MentionPreview.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import { useFormatTime, useFormatNumber } from '@/utils/i18n'
import { getUserId, userUrl, getCommentImages } from '@/utils/commentDisplay'

const props = defineProps({
  comment: {
    type: Object,
    required: true
  },
  // root = 顶层评论（36px 头像），reply = 回复（28px 头像、作者名小一号）
  variant: {
    type: String,
    default: 'root'
  },
  replyToName: {
    type: String,
    default: null
  },
  replyToUserId: {
    type: [String, Number],
    default: null
  },
  highlighted: {
    type: Boolean,
    default: false
  },
  // 是否渲染 id="comment-<id>" 定位锚点；同一评论在页面上只能有一处锚点
  anchor: {
    type: Boolean,
    default: true
  },
  language: {
    type: String,
    default: 'zh-CN'
  }
})

const emit = defineEmits(['like', 'reply'])

const { t } = useI18n()
const { formatTime } = useFormatTime()
const { formatNumber } = useFormatNumber()

const isReply = computed(() => props.variant === 'reply')
const images = computed(() => getCommentImages(props.comment))
</script>

<style scoped>
.comment-item {
  display: flex;
  gap: 12px;
  padding: 16px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  /* 定位滚动居中时避开吸顶 header */
  scroll-margin-top: calc(var(--header-height) + 12px);
}

/* 通知定位高亮：主题色呼吸背景，2s 淡出为透明 */
.comment-highlight {
  border-radius: 8px;
  animation: comment-highlight-flash 2s ease-out;
}

@keyframes comment-highlight-flash {
  0% {
    background: rgba(102, 234, 194, 0.22);
  }
  100% {
    background: transparent;
  }
}

.comment-item:last-child {
  border-bottom: none;
}

.comment-body {
  flex: 1;
  min-width: 0;
}

.comment-author-row {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.comment-author {
  font-size: 15px;
  font-weight: 500;
  color: rgba(255, 255, 255, 0.85);
  text-decoration: none;
}

.reply-item .comment-author {
  font-size: 14px;
}

.comment-author.clickable {
  cursor: pointer;
  transition: color 0.2s;
}

@media (hover: hover) {
  .comment-author.clickable:hover {
    color: #63e2b7;
  }
}

.reply-arrow {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.35);
}

.reply-to-name {
  color: #63e2b7;
}

.comment-time {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.4);
}

.comment-content {
  font-size: 15px;
  color: rgba(255, 255, 255, 0.75);
  line-height: 1.6;
  word-break: break-word;
}

.comment-content :deep(.md-editor) {
  background: transparent !important;
}

.comment-content :deep(.md-editor-preview) {
  padding: 0 !important;
  font-size: 15px;
}

.comment-content :deep(.md-editor-preview-wrapper) {
  padding: 0 !important;
}

/* 抽屉传送到 body 后不在 CommentList 子树内，正文的暗色变量需由本组件自带 */
.comment-content :deep(.md-editor-dark) {
  --md-bk-color: rgb(24, 24, 28);
  --md-scrollbar-bg-color: rgb(24, 24, 28);
}

.comment-content :deep(.md-editor-dark .md-editor-preview) {
  --md-theme-bg-color: rgb(24, 24, 28);
}

.comment-actions {
  display: flex;
  gap: 16px;
  margin-top: 8px;
}

.comment-action-btn {
  color: rgba(255, 255, 255, 0.45) !important;
  font-size: 13px;
}

@media (hover: hover) {
  .comment-action-btn:hover {
    color: rgba(255, 255, 255, 0.7) !important;
  }
}

.comment-action-btn.liked-btn {
  color: #f472b6 !important;
}

@media (hover: hover) {
  .comment-action-btn.liked-btn:hover {
    color: #ec4899 !important;
  }
}
</style>
