<template>
  <div class="comment-replies" :class="[`comment-replies--${variant}`, { 'replies-loading': busy }]">
    <!-- 回复列表头部：排序切换 -->
    <div class="comment-replies-header">
      <div class="replies-sort-toggle">
        <span
          class="sort-option"
          :class="{ active: data.sort === 1, disabled: data.sortLoading }"
          @click="data.sort !== 1 && !data.sortLoading && emit('toggle-sort')"
        >
          {{ t('comment.sort.newest') }}
        </span>
        <span class="sort-divider">/</span>
        <span
          class="sort-option"
          :class="{ active: data.sort === 0, disabled: data.sortLoading }"
          @click="data.sort !== 0 && !data.sortLoading && emit('toggle-sort')"
        >
          {{ t('comment.sort.hottest') }}
        </span>
      </div>
    </div>

    <!-- 抽屉首次加载 -->
    <div v-if="isSheet && data.loading && !replies.length" class="replies-initial-loading">
      <NSpin :size="20" />
    </div>

    <CommentItem
      v-for="reply in replies"
      :key="reply.id"
      :comment="reply"
      variant="reply"
      :reply-to-name="getReplyToName(reply, replies)"
      :reply-to-user-id="getReplyToUserId(reply, replies)"
      :highlighted="highlightCommentId === reply.id"
      :language="language"
      @like="emit('like', $event)"
      @reply="emit('open-reply', $event)"
    >
      <!-- 桌面内联回复编辑器（回复子评论；sheet 里由 open-reply 打开底部输入抽屉） -->
      <CommentReplyEditor
        v-if="!isSheet && activeReplyId === reply.id"
        :post-id="postId"
        :circle-id="circleId"
        :root-id="root.id"
        :reply-to-id="reply.id"
        :reply-to-name="reply.author_name"
        :language="language"
        @submit="emit('reply-submit', $event)"
        @cancel="emit('reply-cancel')"
      />
    </CommentItem>

    <!-- 抽屉：向下追加加载 -->
    <div v-if="isSheet" class="replies-sheet-footer">
      <NSpin v-if="data.pageLoading" :size="16" />
      <button v-else-if="sheetHasMore" type="button" class="load-more-replies-btn" @click="emit('load-more')">
        {{ t('comment.list.loadMoreReplies') }}
      </button>
      <span v-else-if="replies.length" class="replies-no-more">{{ t('common.noMore') }}</span>
      <!-- 首次加载失败：抽屉没有收起 / 重新展开的入口，给一个重试 -->
      <button v-else-if="!data.loading && root.reply_count > 0" type="button" class="load-more-replies-btn" @click="emit('retry')">
        {{ t('comment.list.retryLoadReplies') }}
      </button>
    </div>

    <!-- 内联：收起回复和翻页 -->
    <div v-else class="replies-footer">
      <div class="collapse-replies-btn" @click="emit('collapse')">
        {{ t('comment.list.collapseReplies') }}
      </div>
      <div v-if="data.pages.length > 1 || hasNextPage" class="replies-pagination-arrows">
        <NSpin v-if="data.pageLoading" :size="14" />
        <template v-else>
          <!-- 上一页 -->
          <button
            class="page-arrow-btn"
            :disabled="!hasPrevPage"
            @click="emit('prev-page')"
          >
            <NIcon size="14">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <polyline points="15 18 9 12 15 6"></polyline>
              </svg>
            </NIcon>
          </button>
          <!-- 页码显示 -->
          <span class="page-number">
            {{ data.currentPage + 1 }} / {{ Math.max(1, Math.ceil(root.reply_count / 10)) }}
          </span>
          <!-- 下一页 -->
          <button
            class="page-arrow-btn"
            :disabled="!hasNextPage"
            @click="emit('next-page')"
          >
            <NIcon size="14">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <polyline points="9 18 15 12 9 6"></polyline>
              </svg>
            </NIcon>
          </button>
        </template>
      </div>
    </div>

    <!-- 加载遮罩（抽屉翻页走底部 spinner，只在切换排序时遮罩） -->
    <div v-if="isSheet ? data.sortLoading : busy" class="replies-loading-overlay">
      <NSpin :size="24" />
    </div>
  </div>
</template>

<script setup>
// 一条根评论的回复区：排序切换 + 回复列表 + 内联回复输入（仅 inline）+ 翻页。
// 分页状态（data）归 CommentList 所有，本组件只读展示，变更一律 emit 给父组件。
//   inline：桌面端内联在根评论下方，按页翻页（上一页 / 下一页），可收起
//   sheet ：移动端底部抽屉内，已加载的页依次拼接，底部「加载更多」向下追加
import { computed } from 'vue'
import { NIcon, NSpin } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import CommentItem from '@/components/post/detail/CommentItem.vue'
import CommentReplyEditor from '@/components/post/detail/CommentReplyEditor.vue'
import { getReplyToName, getReplyToUserId } from '@/utils/commentDisplay'

const props = defineProps({
  root: {
    type: Object,
    required: true
  },
  // CommentList 中该根评论的分页数据：{ pages, currentPage, hasMoreMap, loading, pageLoading, sortLoading, sort }
  data: {
    type: Object,
    required: true
  },
  variant: {
    type: String,
    default: 'inline'
  },
  postId: {
    type: String,
    required: true
  },
  circleId: {
    type: String,
    default: null
  },
  language: {
    type: String,
    default: 'zh-CN'
  },
  highlightCommentId: {
    type: String,
    default: ''
  },
  activeReplyId: {
    type: String,
    default: null
  }
})

const emit = defineEmits([
  'toggle-sort',
  'prev-page',
  'next-page',
  'load-more',
  'retry',
  'collapse',
  'like',
  'open-reply',
  'reply-submit',
  'reply-cancel'
])

const { t } = useI18n()

const isSheet = computed(() => props.variant === 'sheet')

// 当前展示的回复：inline 只取当前页；sheet 拼接全部已加载页
// （定位加载时之前的页为 null 占位，跳过；新发回复插在第 0 页头部，可能与后续页重复，按 id 去重）
const replies = computed(() => {
  const { pages, currentPage } = props.data
  if (!isSheet.value) return pages[currentPage] || []
  const seen = new Set()
  const list = []
  for (const page of pages) {
    if (!page) continue
    for (const reply of page) {
      if (seen.has(reply.id)) continue
      seen.add(reply.id)
      list.push(reply)
    }
  }
  return list
})

const hasNextPage = computed(() => props.data.hasMoreMap[props.data.currentPage] || false)

// 是否有上一页（定位加载时之前的页为 null 占位，不可回翻——单向翻页约定）
const hasPrevPage = computed(() => props.data.currentPage > 0 && !!props.data.pages[props.data.currentPage - 1])

// 抽屉：最后一个已加载页之后是否还有数据
const sheetHasMore = computed(() => props.data.hasMoreMap[props.data.pages.length - 1] || false)

const busy = computed(() => props.data.pageLoading || props.data.sortLoading)
</script>

<style scoped>
/* 子评论区 */
.comment-replies {
  position: relative;
}

.comment-replies--inline {
  margin-top: 12px;
  padding: 12px 16px;
  background: rgba(255, 255, 255, 0.03);
  border-radius: 12px;
}

/* 加载状态下的遮罩效果 */
.comment-replies.replies-loading {
  pointer-events: none;
}

/* 加载遮罩层 */
.replies-loading-overlay {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.4);
  border-radius: 12px;
  z-index: 10;
}

/* 回复列表头部 */
.comment-replies-header {
  display: flex;
  justify-content: flex-start;
  margin-bottom: 4px;
  padding-bottom: 4px;
}

/* CommentItem 根元素带本组件 scope，可直接命中 */
.comment-replies .comment-item {
  padding: 10px 0;
  border-bottom: 1px solid rgba(255, 255, 255, 0.04);
}

.comment-replies .comment-item:last-child {
  border-bottom: none;
}

/* 抽屉内滚动容器是抽屉本身，不需要避让吸顶 header */
.comment-replies--sheet .comment-item {
  scroll-margin-top: 0;
}

/* 收起回复和翻页容器 */
.replies-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 8px;
}

/* 回复排序切换器 */
.replies-sort-toggle {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
}

.sort-option {
  color: rgba(255, 255, 255, 0.4);
  cursor: pointer;
  transition: color 0.2s;
}

@media (hover: hover) {
  .sort-option:hover {
    color: rgba(255, 255, 255, 0.6);
  }
}

.sort-option.active {
  color: #63e2b7;
}

.sort-option.disabled {
  opacity: 0.4;
  pointer-events: none;
  cursor: not-allowed;
}

.sort-divider {
  color: rgba(255, 255, 255, 0.2);
}

/* 收起回复 */
.collapse-replies-btn {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.4);
  cursor: pointer;
  user-select: none;
}

@media (hover: hover) {
  .collapse-replies-btn:hover {
    color: rgba(255, 255, 255, 0.6);
  }
}

/* 翻页箭头容器 */
.replies-pagination-arrows {
  display: flex;
  align-items: center;
  gap: 8px;
}

/* 翻页箭头按钮 */
.page-arrow-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 6px;
  color: rgba(255, 255, 255, 0.6);
  cursor: pointer;
  transition: all 0.2s;
}

@media (hover: hover) {
  .page-arrow-btn:hover:not(:disabled) {
    background: rgba(99, 226, 183, 0.1);
    border-color: rgba(99, 226, 183, 0.3);
    color: #63e2b7;
  }
}

.page-arrow-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}

/* 页码显示 */
.page-number {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.5);
  min-width: 40px;
  text-align: center;
}

/* 抽屉：首次加载 / 底部加载更多 */
.replies-initial-loading,
.replies-sheet-footer {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 44px;
  padding: 8px 0;
}

.load-more-replies-btn {
  min-height: 36px;
  padding: 0 16px;
  background: rgba(102, 234, 194, 0.08);
  border: 1px solid rgba(102, 234, 194, 0.2);
  border-radius: 18px;
  color: #66eac2;
  font-size: 13px;
  cursor: pointer;
}

.replies-no-more {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.35);
}
</style>
