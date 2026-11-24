<template>
  <article class="m-post-item" :class="[{ flush }, tone && `is-tone-${tone}`]">
    <!-- 移动端紧凑帖子列表项（PostCard / 我的帖子 / 收藏 / 浏览历史在移动端外框下共用）。
         单行头部 + 标题、摘要各 2 行 + 右侧缩略图 + 小号统计栏，一屏约 4 条。
         整项由封面链接承担跳转；头部有链接的来源抬层各自响应，无链接时渲染普通 div、点击落到封面链接 -->
    <SmartLink class="m-cover-link" :to="`/post/${postId}`" :aria-label="title" />

    <header class="m-meta">
      <component :is="primaryTo ? SmartLink : 'div'" class="m-source" v-bind="primaryTo ? { to: primaryTo } : {}">
        <NAvatar round :size="22" :src="primaryAvatar || undefined">
          <span v-if="!primaryAvatar" class="m-avatar-fallback">{{ primaryName?.charAt(0) }}</span>
        </NAvatar>
        <span class="m-source-name">{{ primaryName }}</span>
      </component>
      <template v-if="secondaryName">
        <span class="m-dot">·</span>
        <component :is="secondaryTo ? SmartLink : 'span'" class="m-secondary" v-bind="secondaryTo ? { to: secondaryTo } : {}">
          {{ secondaryName }}
        </component>
      </template>
      <span v-if="timeText" class="m-dot">·</span>
      <span v-if="timeText" class="m-time">{{ timeText }}</span>
      <div v-if="$slots.meta" class="m-meta-extra">
        <slot name="meta" />
      </div>
      <!-- 「…」更多菜单（信息流开启；举报：已发布且不是自己的帖子才显示），抬层盖住封面链接 -->
      <PostMoreMenu
        v-if="moreMenu"
        class="m-more"
        :post-id="postId"
        :author-id="authorId"
        :status="status"
      />
    </header>

    <div class="m-body">
      <div class="m-text">
        <h3 class="m-title">
          <span v-if="pinned" class="m-badge is-pinned">{{ t('post.badges.pinned') }}</span>
          <span v-if="essence" class="m-badge is-essence">{{ t('post.badges.essence') }}</span>
          {{ title }}
        </h3>
        <p v-if="content" class="m-summary">{{ content }}</p>
      </div>
      <div v-if="images.length" class="m-thumb">
        <img :src="images[0]" alt="" loading="lazy" />
        <span v-if="images.length > 1" class="m-thumb-count">
          <NIcon size="12"><PhotoIcon /></NIcon>{{ images.length }}
        </span>
      </div>
    </div>

    <footer class="m-stats">
      <span class="m-stat"><NIcon size="14"><CommentIcon /></NIcon>{{ formatNumber(commentCount) }}</span>
      <span class="m-stat"><NIcon size="14"><HeartIcon /></NIcon>{{ formatNumber(likeCount) }}</span>
      <span class="m-stat"><NIcon size="14"><EyeIcon /></NIcon>{{ formatNumber(viewCount) }}</span>
      <!-- 未发布的帖子不可分享 -->
      <ShareButton
        v-if="shareable && (status === null || status === 1)"
        class="m-share"
        variant="compact"
        :post-id="postId"
        :title="title"
        :share-count="shareCount"
        :is-shared="isShared"
      />
    </footer>
  </article>
</template>

<script setup>
import { NAvatar, NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  Eye as EyeIcon,
  Heart as HeartIcon,
  MessageCircle as CommentIcon,
  Photo as PhotoIcon
} from '@vicons/tabler'
import SmartLink from '@/components/common/SmartLink.vue'
import PostMoreMenu from '@/components/post/PostMoreMenu.vue'
import ShareButton from '@/components/post/ShareButton.vue'
import { useFormatNumber } from '@/utils/i18n'

defineProps({
  postId: { type: String, required: true },
  title: { type: String, default: '' },
  content: { type: String, default: '' },
  images: { type: Array, default: () => [] },
  // 头部主来源（带头像）：信息流里是圈子，圈子内 / 个人页里是作者
  primaryName: { type: String, default: '' },
  primaryAvatar: { type: String, default: '' },
  primaryTo: { type: String, default: null },
  // 头部次要来源（无头像）：信息流里是作者，个人页里是圈子
  secondaryName: { type: String, default: '' },
  secondaryTo: { type: String, default: null },
  timeText: { type: String, default: '' },
  pinned: { type: Boolean, default: false },
  essence: { type: Boolean, default: false },
  viewCount: { type: Number, default: 0 },
  likeCount: { type: Number, default: 0 },
  commentCount: { type: Number, default: 0 },
  // 分享按钮；shareCount 为 null 时只显示图标，isShared 缺省按未分享
  shareable: { type: Boolean, default: false },
  shareCount: { type: Number, default: null },
  isShared: { type: Boolean, default: false },
  // 通栏：去掉圆角与左右边框，用于左右无边距的列表（如「我的」页）
  flush: { type: Boolean, default: false },
  // 状态色调（「我的帖子」里驳回 = warning / 屏蔽 = error）：左侧色条 + 正文降低不透明度
  tone: { type: String, default: '', validator: (v) => ['', 'warning', 'error'].includes(v) },
  // 头部右侧「…」更多菜单（信息流卡片开启；「我的帖子」等作者自己的列表不开）
  moreMenu: { type: Boolean, default: false },
  authorId: { type: String, default: null },
  status: { type: Number, default: null }
})

const { t } = useI18n()
const { formatNumber } = useFormatNumber()
</script>

<style scoped>
.m-post-item {
  position: relative;
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 8px;
  padding: 12px;
  border-radius: 12px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.06);
}

.m-post-item:active {
  background: rgba(255, 255, 255, 0.06);
}

.m-post-item.flush {
  margin-bottom: 0;
  border-radius: 0;
  border-width: 0 0 1px;
}

/* 状态色调：左侧色条（inset 阴影，不改变布局），正文 / 缩略图变淡；头部状态标签保持清晰 */
.m-post-item.is-tone-warning {
  box-shadow: inset 3px 0 0 #fbbf24;
}

.m-post-item.is-tone-error {
  box-shadow: inset 3px 0 0 #f87171;
}

.m-post-item[class*='is-tone-'] .m-body {
  opacity: 0.6;
}

/* 整项封面链接（stretched-link），头部链接抬层盖住它 */
.m-cover-link {
  position: absolute;
  inset: 0;
  z-index: 1;
}

.m-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  font-size: 12px;
  line-height: 22px;
  color: var(--text-tertiary);
  white-space: nowrap;
}

.m-source,
.m-secondary {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  color: inherit;
  text-decoration: none;
}

/* 只有可跳转的来源抬层盖住封面链接 */
a.m-source,
a.m-secondary {
  position: relative;
  z-index: 2;
}

.m-source {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-shrink: 1;
}

.m-source :deep(.n-avatar) {
  flex-shrink: 0;
}

.m-avatar-fallback {
  font-size: 11px;
  font-weight: 700;
  color: #fff;
}

.m-source-name {
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 13px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.85);
}

.m-dot {
  flex-shrink: 0;
  opacity: 0.6;
}

.m-time {
  flex-shrink: 0;
}

.m-meta-extra {
  position: relative;
  z-index: 2;
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  margin-left: auto;
}

/* 更多菜单：贴右，按钮 28px，负边距不撑高单行头部（22px） */
.m-more {
  margin: -3px -6px -3px auto;
}

.m-meta-extra ~ .m-more {
  margin-left: 0;
}

.m-body {
  display: flex;
  gap: 12px;
}

.m-text {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.m-title,
.m-summary {
  margin: 0;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
}

.m-title {
  font-size: 15px;
  font-weight: 600;
  line-height: 1.4;
  color: rgba(255, 255, 255, 0.95);
}

.m-summary {
  font-size: 13px;
  line-height: 1.5;
  color: rgba(255, 255, 255, 0.65);
}

.m-badge {
  display: inline-block;
  margin-right: 4px;
  padding: 0 5px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 500;
  line-height: 17px;
  vertical-align: 2px;
}

.m-badge.is-pinned {
  color: #f59e0b;
  background: rgba(245, 158, 11, 0.15);
}

.m-badge.is-essence {
  color: #66eac2;
  background: rgba(102, 234, 194, 0.15);
}

.m-thumb {
  position: relative;
  flex-shrink: 0;
  width: 84px;
  height: 84px;
  border-radius: 8px;
  overflow: hidden;
  background: var(--bg-tertiary);
}

.m-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.m-thumb-count {
  position: absolute;
  right: 4px;
  bottom: 4px;
  display: flex;
  align-items: center;
  gap: 2px;
  padding: 0 5px;
  border-radius: 8px;
  font-size: 11px;
  line-height: 16px;
  color: #fff;
  background: rgba(0, 0, 0, 0.55);
}

.m-stats {
  display: flex;
  gap: 18px;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
}

.m-stat {
  display: flex;
  align-items: center;
  gap: 4px;
}

/* 分享靠右，与其它只读统计区分开 */
.m-share {
  margin-left: auto;
}
</style>
