<template>
  <NCard :bordered="false" class="post-card">
    <!-- 帖子头部信息 -->
    <div class="post-header">
      <!-- 置顶、精华、状态标签 -->
      <div class="post-badges">
        <NTag v-if="post.is_pinned" type="warning" size="small" round>{{ t('post.badges.pinned') }}</NTag>
        <NTag v-if="post.is_essence" type="success" size="small" round>{{ t('post.badges.essence') }}</NTag>
        <NTag v-if="post.is_lock" type="error" size="small" round>{{ t('post.badges.locked') }}</NTag>
      </div>

      <!-- 标题 + 右侧「…」更多菜单（举报：已发布且不是自己的帖子才显示） -->
      <div class="post-title-row">
        <h1 class="post-title">{{ post.title }}</h1>
        <PostMoreMenu
          class="post-more"
          :post-id="postId || post.id"
          :author-id="post.author_id"
          :status="post.status"
          size="medium"
        />
      </div>
      <!-- 状态标签 -->
      <NTag
        v-if="post.status !== 1"
        :type="getPostStatusInfo(post.status).type"
        size="large"
        round
      >
        {{ getPostStatusInfo(post.status).text }}
      </NTag>
      <!-- 作者本人：审核中 / 驳回 / 屏蔽提示条（status=2 时带刷新按钮）；说明已在提示条里，标签不再带气泡 -->
      <PostStatusBanner
        v-if="isAuthor"
        class="post-status-banner-slot"
        :status="post.status"
        :slow="reviewSlow"
        :refreshing="refreshing"
        @refresh="$emit('refresh')"
      />

      <!-- 作者信息 -->
      <div class="post-meta">
        <div class="author-info">
          <NAvatar
            round
            :size="40"
            :src="post.author_avatar || '/default-avatar.png'"
          />
          <div class="author-text">
            <SmartLink class="author-name" :class="{ clickable: post.author_id }" :to="post.author_id ? `/user/${post.author_id}` : null">
              {{ post.author_name || t('user.anonymous') }}
            </SmartLink>
            <div class="post-time">
              {{ formatTime(post.create_time) }}
            </div>
          </div>
        </div>

        <!-- 统计信息 -->
        <div class="post-stats">
          <div class="stat-item">
            <NIcon class="stat-icon" size="18">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"></path>
                <circle cx="12" cy="12" r="3"></circle>
              </svg>
            </NIcon>
            <span>{{ formatNumber(post.view_count, VIEW_COUNT_CAP) }}</span>
          </div>
          <div class="stat-item">
            <NIcon class="stat-icon" size="18">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"></path>
              </svg>
            </NIcon>
            <span>{{ formatNumber(post.comment_count, STATS_COUNT_CAP) }}</span>
          </div>
          <div class="stat-item">
            <NIcon class="stat-icon" size="18">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
              </svg>
            </NIcon>
            <span>{{ formatNumber(post.like_count, STATS_COUNT_CAP) }}</span>
          </div>
          <div class="stat-item">
            <NIcon class="stat-icon" size="18">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"></path>
              </svg>
            </NIcon>
            <span>{{ formatNumber(post.collect_count, STATS_COUNT_CAP) }}</span>
          </div>
          <div v-if="shareState.count !== null" class="stat-item" :class="{ 'is-shared': shareState.shared }">
            <NIcon class="stat-icon" size="18"><ShareIcon /></NIcon>
            <span>{{ formatNumber(shareState.count, STATS_COUNT_CAP) }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- 帖子正文（Markdown渲染 + @提及链接化） -->
    <div class="post-content">
      <MentionPreview
        :content="post.content"
        :mentions="post.mentions"
        :language="language"
      />
    </div>

    <!-- 底部操作栏：未发布（审核中 / 驳回 / 屏蔽）的帖子后端不接受点赞 / 收藏，直接隐藏 -->
    <div v-if="isPostPublished(post)" class="post-actions">
      <NButton
        size="large"
        :type="post.is_liked ? 'primary' : 'default'"
        @click="$emit('like')"
      >
        <template #icon>
          <NIcon size="20">
            <svg v-if="!post.is_liked" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
            </svg>
            <svg v-else viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="2">
              <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
            </svg>
          </NIcon>
        </template>
        {{ post.is_liked ? t('post.actions.liked') : t('post.actions.like') }}
      </NButton>

      <NButton
        size="large"
        :type="post.is_collected ? 'primary' : 'default'"
        @click="$emit('collect')">
        <template #icon>
          <NIcon size="20">
            <svg v-if="!post.is_collected" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"></path>
            </svg>
            <svg v-else viewBox="0 0 24 24" fill="currentColor" stroke="currentColor" stroke-width="2">
              <path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"></path>
            </svg>
          </NIcon>
        </template>
        {{ post.is_collected ? t('post.actions.favorited') : t('post.actions.favorite') }}
      </NButton>

      <!-- 分享：访客也可分享链接（不计数）；状态与列表卡片共用 usePostShare -->
      <ShareButton
        class="post-share"
        variant="button"
        :post-id="sharePostId"
        :title="post.title"
        :share-count="post.share_count ?? null"
        :is-shared="!!post.is_shared"
      />
    </div>

    <!-- 插槽：评论编辑器和评论列表 -->
    <slot />
  </NCard>
</template>

<script setup>
import { computed } from 'vue'
import { NCard, NTag, NAvatar, NButton, NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import MentionPreview from '@/components/post/detail/MentionPreview.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import PostStatusBanner from '@/components/post/detail/PostStatusBanner.vue'
import PostMoreMenu from '@/components/post/PostMoreMenu.vue'
import ShareButton from '@/components/post/ShareButton.vue'
import { Share as ShareIcon } from '@vicons/tabler'
import { usePostShareState } from '@/composables/usePostShare'
import { isPostPublished } from '@/composables/usePostReview'
import { usePostStatus } from '@/composables/usePostStatus'
import { useFormatTime, useFormatNumber } from '@/utils/i18n'

const props = defineProps({
  post: {
    type: Object,
    required: true
  },
  // 帖子 id（详情页取路由参数，post.id 兜底）
  postId: {
    type: String,
    default: ''
  },
  language: {
    type: String,
    default: 'zh-CN'
  },
  // 当前用户是否为作者：只有作者能看到未发布的帖子及其状态提示条
  isAuthor: {
    type: Boolean,
    default: false
  },
  // 审核中且自动刷新已停止（审核时间较长）
  reviewSlow: {
    type: Boolean,
    default: false
  },
  refreshing: {
    type: Boolean,
    default: false
  }
})

defineEmits(['like', 'collect', 'refresh'])

const { t } = useI18n()
const { formatTime } = useFormatTime()
const { formatNumber } = useFormatNumber()
const { getPostStatusInfo } = usePostStatus()

const VIEW_COUNT_CAP = 1_000_000_000
const STATS_COUNT_CAP = 100_000_000

// 分享：分享数 / 是否已分享合并会话内的分享结果，与列表卡片保持一致
const sharePostId = computed(() => props.postId || props.post.id)
const shareState = usePostShareState(
  sharePostId,
  () => props.post.share_count,
  () => props.post.is_shared
)
</script>

<style scoped>
.post-card {
  border-radius: 16px !important;
  overflow: hidden;
}

.post-header {
  /* margin-bottom: 24px; */
  padding: 24px 24px 0 24px;
}

.post-badges {
  display: flex;
  gap: 8px;
  margin-bottom: 16px;
}

.post-title-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  margin-bottom: 16px;
}

.post-title-row .post-title {
  flex: 1;
  min-width: 0;
  margin-bottom: 0;
  word-break: break-word;
}

.post-more {
  /* 与首行标题垂直居中（32px 字号 × 1.4 行高） */
  margin-top: 6px;
  margin-right: -6px;
}

.post-title {
  font-size: 32px;
  font-weight: 600;
  margin: 0 0 16px 0;
  line-height: 1.4;
  color: rgba(255, 255, 255, 0.9);
}

.post-status-banner-slot {
  margin-top: 12px;
}

.post-meta {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 16px 0;
}

.author-info {
  display: flex;
  align-items: center;
  gap: 12px;
}

.author-text {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.author-name {
  font-size: 18px;
  font-weight: 500;
  color: rgba(255, 255, 255, 0.9);
  text-decoration: none;
  transition: color 0.2s;
}

.author-name.clickable {
  cursor: pointer;
}

@media (hover: hover) {
  .author-name.clickable:hover {
    color: #63e2b7;
  }
}

.post-time {
  font-size: 14px;
  color: rgba(255, 255, 255, 0.5);
}

.post-stats {
  display: flex;
  gap: 24px;
}

.stat-item {
  display: flex;
  align-items: center;
  gap: 6px;
  color: rgba(255, 255, 255, 0.6);
  font-size: 16px;
}

.stat-icon {
  color: rgba(255, 255, 255, 0.5);
}

.stat-item.is-shared,
.stat-item.is-shared .stat-icon {
  color: #66eac2;
}

.post-content {
  margin: 14px 0;
  padding: 24px;
  padding-top: 0;
  border-radius: 15px;
}

:deep(.md-editor *) {
  font-family: 'Fira Code', 'Cascadia Code', 'Maple Mono NF CN', 'JetBrains Mono', 'Source Code Pro', 'Menlo', 'Monaco', 'Consolas', 'Courier New', 'PingFang SC', 'Microsoft YaHei', monospace;
}

.post-actions {
  display: flex;
  justify-content: center;
  gap: 16px;
  padding: 16px 0;
}

.post-actions .n-button {
  min-width: 120px;
  border-radius: 10px;
  font-weight: 500;
}

:deep(.n-card__content) {
  padding: 32px;
}

:deep(.md-editor-dark) {
  --md-bk-color: rgb(24, 24, 28);
  --md-scrollbar-bg-color: rgb(24, 24, 28);
}

:deep(.md-editor-dark .md-editor-preview) {
  --md-theme-bg-color: rgb(24, 24, 28);
}

@media (max-width: 1200px) {
  .post-meta {
    flex-direction: column;
    align-items: flex-start;
    gap: 16px;
  }
}

@media (max-width: 768px) {
  :deep(.n-card__content) {
    padding: 12px;
  }

  .post-header {
    padding: 12px 12px 0;
  }

  .post-content {
    padding: 12px;
    padding-top: 0;
  }

  .post-actions .n-button {
    min-width: 0;
    flex: 1;
    height: 44px;
  }

  .post-actions .post-share {
    flex: 1;
  }

  .post-actions .post-share :deep(.n-button) {
    min-width: 0;
    height: 44px;
  }

  .post-title {
    font-size: 22px;
  }

  .post-more {
    margin-top: 0;
  }

  .post-stats {
    flex-wrap: wrap;
    gap: 16px;
  }
}
</style>
