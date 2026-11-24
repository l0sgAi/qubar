<template>
  <NCard ref="commentListCardRef" :bordered="false" class="comment-list-card">
    <div class="comment-list-header">
      <span class="comment-section-title">{{ t('comment.list.title', { count: props.commentCount }) }}</span>
      <div class="comment-sort">
        <NButton text size="small" :type="sort === 'hottest' ? 'primary' : 'default'" @click="$emit('update:sort', 'hottest')">{{ t('comment.sort.hottest') }}</NButton>
        <NDivider vertical />
        <NButton text size="small" :type="sort === 'newest' ? 'primary' : 'default'" @click="$emit('update:sort', 'newest')">{{ t('comment.sort.newest') }}</NButton>
      </div>
    </div>

    <div class="comment-list">
      <CommentItem
        v-for="comment in comments"
        :key="comment.id"
        :comment="comment"
        :highlighted="highlightCommentId === comment.id"
        :language="language"
        @like="handleToggleLike('comment', $event)"
        @reply="openReply"
      >
        <!-- 桌面内联回复编辑器（移动端回复走底部输入抽屉 MobileCommentComposer） -->
        <CommentReplyEditor
          v-if="!mobileShell && activeReplyId === comment.id"
          :post-id="postId"
          :circle-id="circleId"
          :root-id="comment.id"
          :reply-to-id="null"
          :reply-to-name="comment.author_name"
          :language="language"
          @submit="handleReplySubmit(comment, $event)"
          @cancel="activeReplyId = null"
        />

        <!-- 点击查看回复：桌面内联展开；移动端打开底部抽屉 -->
        <div
          v-if="comment.reply_count > 0 && (mobileShell || !isRepliesExpanded(comment.id))"
          class="load-replies-btn"
          @click="mobileShell ? openReplySheet(comment) : loadReplies(comment)"
        >
          <NSpin v-if="!mobileShell && loadingReplies[comment.id]" :size="14" />
          <span v-else>{{ t('comment.list.viewReplies', { count: comment.reply_count }) }}</span>
        </div>

        <!-- 子评论（桌面内联） -->
        <CommentReplies
          v-if="!mobileShell && isRepliesExpanded(comment.id)"
          :root="comment"
          :data="getRepliesData(comment.id)"
          :post-id="postId"
          :circle-id="circleId"
          :language="language"
          :highlight-comment-id="highlightCommentId"
          :active-reply-id="activeReplyId"
          @toggle-sort="toggleRepliesSort(comment.id)"
          @prev-page="prevPage(comment.id)"
          @next-page="nextPage(comment.id)"
          @collapse="collapseReplies(comment.id)"
          @like="handleToggleLike('comment', $event)"
          @open-reply="openReply"
          @reply-submit="handleReplySubmit(comment, $event)"
          @reply-cancel="activeReplyId = null"
        />
      </CommentItem>

      <!-- 无限滚动触发器 -->
      <div v-if="hasMore && !loading" class="load-more-trigger" ref="loadMoreTrigger"></div>

      <!-- 加载状态 -->
      <div v-if="loading" class="loading-state">
        <NSpin size="medium" />
      </div>

      <!-- 没有更多 -->
      <div v-if="!hasMore && comments.length > 0" class="no-more">
        <p>{{ t('comment.list.noMore') }}</p>
      </div>

      <NEmpty v-if="!loading && comments.length === 0 && initialized" :description="t('comment.list.empty')" >
      <template #icon>
          <NIcon>
            <CommentRound/>
          </NIcon>
        </template>
      </NEmpty> 
    </div>

    <!-- 移动端：一条评论的全部回复在底部抽屉中查看（开合跟随路由 query，返回手势即关闭） -->
    <NDrawer
      v-if="mobileShell"
      :show="!!sheetComment"
      placement="bottom"
      height="82%"
      class="reply-sheet"
      :auto-focus="false"
      :trap-focus="false"
      @update:show="(show) => !show && closeReplySheet()"
      @after-enter="handleSheetAfterEnter"
      @after-leave="handleSheetAfterLeave"
    >
      <NDrawerContent v-if="sheetDisplayComment" :native-scrollbar="true" body-content-class="reply-sheet-body">
        <template #header>
          <div class="reply-sheet-header">
            <span class="reply-sheet-title">{{ t('comment.list.repliesTitle', { count: sheetDisplayComment.reply_count }) }}</span>
            <button type="button" class="reply-sheet-close" :aria-label="t('comment.list.closeReplies')" @click="closeReplySheet">
              <NIcon size="20">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <line x1="18" y1="6" x2="6" y2="18"></line>
                  <line x1="6" y1="6" x2="18" y2="18"></line>
                </svg>
              </NIcon>
            </button>
          </div>
        </template>

        <!-- 根评论作为上下文；锚点留给主列表那一份 -->
        <CommentItem
          :comment="sheetDisplayComment"
          :anchor="false"
          :language="language"
          class="reply-sheet-root"
          @like="handleToggleLike('comment', $event)"
          @reply="openReply"
        />

        <CommentReplies
          variant="sheet"
          :root="sheetDisplayComment"
          :data="getRepliesData(sheetDisplayComment.id)"
          :post-id="postId"
          :circle-id="circleId"
          :language="language"
          :highlight-comment-id="highlightCommentId"
          @toggle-sort="toggleRepliesSort(sheetDisplayComment.id)"
          @load-more="loadMoreReplies(sheetDisplayComment.id)"
          @retry="loadReplies(sheetDisplayComment)"
          @like="handleToggleLike('comment', $event)"
          @open-reply="openReplyToReply(sheetDisplayComment, $event)"
        />

        <!-- 底部输入条：回复根评论 -->
        <template #footer>
          <MobileComposeBar
            :placeholder="t('mobile.comment.replyTitle', { name: sheetDisplayComment.author_name })"
            @compose="openReply(sheetDisplayComment)"
          />
        </template>
      </NDrawerContent>
    </NDrawer>

    <!-- 移动端：评论 / 回复的输入抽屉（顶层评论由 PostDetail 底部输入条经 openComposer 打开） -->
    <MobileCommentComposer
      v-if="mobileShell"
      ref="composerRef"
      :post-id="postId"
      @submit="handleComposerSubmit"
      @closed="handleComposerClosed"
    />
  </NCard>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, watch, nextTick } from 'vue'
import { NCard, NButton, NDivider, NIcon, NEmpty, NSpin, NDrawer, NDrawerContent, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import CommentItem from '@/components/post/detail/CommentItem.vue'
import CommentReplies from '@/components/post/detail/CommentReplies.vue'
import { getCommentList, getCommentReplies, locateComment, isCommentUnavailableError } from '@/api/comment'
import { seedContentMentions } from '@/utils/mentionResolve'
import { toggleLike } from '@/api/like'
import CommentReplyEditor from '@/components/post/detail/CommentReplyEditor.vue'
import MobileComposeBar from '@/components/post/detail/MobileComposeBar.vue'
import MobileCommentComposer, { COMPOSE_QUERY_KEY } from '@/components/post/detail/MobileCommentComposer.vue'
import { useMobileShell } from '@/composables/useAppShell'
import {CommentRound} from '@vicons/material'
import { useInteractionToggle, interactionErrorMessage } from '@/composables/useInteractionToggle'
import { auth } from '@/utils/auth'
import { requireLogin } from '@/utils/guest-action'

// 移动端门户：评论 / 回复改用底部输入抽屉里的纯文本输入（docs/mobile-adaptation-plan.md 6.6）
const mobileShell = useMobileShell()

const props = defineProps({
  postId: {
    type: String,
    required: true
  },
  // 帖子所属圈子 uuid：回复 @选人按圈子作用域搜索（null = 全站搜索）
  circleId: {
    type: String,
    default: null
  },
  sort: {
    type: String,
    default: 'hottest'
  },
  language: {
    type: String,
    default: 'zh-CN'
  },
  commentCount: {
    type: Number,
    default: 0
  },
  // 通知点击直达：目标评论ID（顶层或回复），空串 = 不定位
  locateCommentId: {
    type: String,
    default: ''
  }
})

const emit = defineEmits(['update:sort', 'update:commentCount'])

const { t } = useI18n()
const message = useMessage()
const route = useRoute()
const router = useRouter()

// 通知定位状态：高亮目标评论ID / 定位流程进行中（防重入）
const highlightCommentId = ref('')
const locating = ref(false)
// 定位版本号：新目标到来即在飞流程失效（防旧流程完成后回灌/滚动到过期目标）
let locateVersion = 0
// 在飞期间到来的最新目标，流程结束后补跑（只保留最新一个）
let pendingLocateId = ''

// 数据状态
const comments = ref([])
const loading = ref(false)
const hasMore = ref(false)
const nextCursor = ref('')
const initialized = ref(false)

// 回复展开状态和分页数据（游标分页 + 页面缓存）
// 格式: { [commentId]: { pages: [[]], currentPage: 0, cursors: [''], hasMoreMap: {}, loading: false, sort: 1 } }
const repliesData = ref({})
const loadingReplies = ref({})

// 获取某个评论的回复数据
const getRepliesData = (commentId) => {
  if (!repliesData.value[commentId]) {
    repliesData.value[commentId] = {
      pages: [[]],           // 每页的数据
      currentPage: 0,        // 当前页码（从0开始）
      cursors: [''],         // 每页对应的游标
      hasMoreMap: {},        // 每页是否有更多数据 {pageIndex: boolean}
      loading: false,
      pageLoading: false,    // 页面加载状态（用于显示遮罩）
      sortLoading: false,    // 排序切换加载状态
      sort: 1                // 该评论的回复排序：0=最热（最多点赞），1=最新（默认），按评论独立
    }
  }
  return repliesData.value[commentId]
}

// 获取当前显示的回复列表
const getCurrentReplies = (commentId) => {
  const data = getRepliesData(commentId)
  return data.pages[data.currentPage] || []
}

// 是否有下一页
const hasNextPage = (commentId) => {
  const data = getRepliesData(commentId)
  return data.hasMoreMap[data.currentPage] || false
}

// 展开的回复ID列表（用于控制显示）
const expandedReplyIds = ref([])

// 检查回复是否展开
const isRepliesExpanded = (commentId) => {
  return expandedReplyIds.value.includes(commentId)
}

// 展开回复
const expandReplies = (commentId) => {
  if (!expandedReplyIds.value.includes(commentId)) {
    expandedReplyIds.value.push(commentId)
  }
}

// 收起回复
const collapseReplies = (commentId) => {
  expandedReplyIds.value = expandedReplyIds.value.filter(id => id !== commentId)
  // 重置该评论的回复数据，下次展开重新加载
  delete repliesData.value[commentId]
}

// 重置回复数据（切换排序时使用）
const resetRepliesData = (commentId) => {
  delete repliesData.value[commentId]
}

// 桌面内联回复编辑器
const activeReplyId = ref(null)

// 移动端输入抽屉。open 须在点击处理里同步调用（iOS 弹键盘依赖用户手势）
const composerRef = ref(null)
const openComposer = (target) => composerRef.value?.open(target)

// 回复根评论：桌面切换内联编辑器；移动端打开输入抽屉
const openReply = (comment) => {
  if (mobileShell.value) {
    openComposer({ rootId: comment.id, replyToId: null, replyToName: comment.author_name })
    return
  }
  activeReplyId.value = activeReplyId.value === comment.id ? null : comment.id
}

// 移动端回复抽屉里回复某条回复
const openReplyToReply = (root, reply) => {
  openComposer({ rootId: root.id, replyToId: reply.id, replyToName: reply.author_name })
}

// ============ 移动端回复抽屉 ============
// 抽屉开合跟随路由 query.replies=<根评论ID>：打开时 push 一条记录，
// Android 返回手势 / 浏览器后退即关闭抽屉，而不是离开帖子
const SHEET_QUERY_KEY = 'replies'
// 关闭在飞（back 的 popstate 尚未到达）：防止重复关闭多退一步
let sheetClosing = false
// 抽屉进场动画结束后要滚动到的回复ID（通知定位）
let pendingSheetScrollId = ''

// 本实例打开的根评论ID：只认自己打开的，挂载时残留的 query（刷新 / 后退回来）不会弹出抽屉
const sheetOpenId = ref('')

const sheetRouteActive = () => !!sheetOpenId.value && route.query[SHEET_QUERY_KEY] === sheetOpenId.value

const sheetCommentId = computed(() => (mobileShell.value && sheetRouteActive() ? sheetOpenId.value : ''))
const sheetComment = computed(() => {
  if (!sheetCommentId.value) return null
  return comments.value.find(c => c.id === sheetCommentId.value) || null
})

// 抽屉渲染用的根评论：关闭后保留到离场动画结束（after-leave），避免空白面板滑出
const sheetDisplayComment = ref(null)
watch(sheetComment, (comment) => {
  if (comment) sheetDisplayComment.value = comment
})

const scrollToComment = (commentId, options) => {
  document.getElementById(`comment-${commentId}`)?.scrollIntoView({ behavior: 'smooth', block: 'center', ...options })
}

const queryWithoutSheet = () => {
  const { [SHEET_QUERY_KEY]: _omit, ...query } = route.query
  return query
}

// 去掉抽屉 query：上一条历史正是「同页无抽屉」时后退（抽屉是 push 进来的，返回手势一致），
// 否则原地 replace（如刷新后上一条是别的页面），不会把用户退出帖子
const leaveSheetRoute = () => {
  const back = router.options.history.state?.back
  if (back && back === router.resolve({ query: queryWithoutSheet() }).fullPath) {
    router.back()
  } else {
    router.replace({ query: queryWithoutSheet() })
  }
}

// 评论是否在该根评论的抽屉里（根评论本身或已加载的回复）
const isInSheet = (rootId, commentId) => {
  if (!commentId) return false
  if (commentId === rootId) return true
  return !!repliesData.value[rootId]?.pages.some(page => page?.some(r => r.id === commentId))
}

// fromLocate：通知定位已加载好目标页（之前的页为 null 占位），直接复用
const openReplySheet = (comment, { fromLocate = false } = {}) => {
  // 定位留下的跳页数据在抽屉里无法回看前面的页：用户主动打开时丢弃，从第一页重新拉
  if (!fromLocate && repliesData.value[comment.id]?.pages.some(page => page === null)) {
    collapseReplies(comment.id)
  }
  // 首次打开才加载；之后复用缓存（关闭抽屉不清数据，再开即时展示）
  if (!isRepliesExpanded(comment.id)) loadReplies(comment)
  if (sheetCommentId.value === comment.id) return
  const switching = sheetRouteActive()
  sheetOpenId.value = comment.id
  const query = { ...route.query, [SHEET_QUERY_KEY]: comment.id }
  if (switching) {
    // 已开着另一条：原地替换，不叠加历史记录
    router.replace({ query })
  } else {
    router.push({ query })
  }
}

const closeReplySheet = () => {
  if (!sheetRouteActive() || sheetClosing) return
  sheetClosing = true
  leaveSheetRoute()
}

const handleSheetAfterEnter = () => {
  if (!pendingSheetScrollId) return
  scrollToComment(pendingSheetScrollId)
  pendingSheetScrollId = ''
}

const handleSheetAfterLeave = () => {
  if (!sheetComment.value) sheetDisplayComment.value = null
}

// 抽屉关闭（返回手势 / 后退 / 关闭按钮，query 被移除）：复位抽屉状态，清掉抽屉里的高亮
// （回复草稿在输入抽屉里按目标保留，不受影响）
watch(() => route.query[SHEET_QUERY_KEY], (id) => {
  if (id || !sheetOpenId.value) return
  const rootId = sheetOpenId.value
  sheetOpenId.value = ''
  sheetClosing = false
  pendingSheetScrollId = ''
  if (isInSheet(rootId, highlightCommentId.value)) highlightCommentId.value = ''
})

// 去掉抽屉相关 query（回复抽屉 + 输入抽屉）：刷新 / 后退回来的残留，或跨到桌面断点。
// 上一条历史正是「同页无抽屉」时后退，否则原地 replace
const leaveOverlayRoutes = () => {
  const { [SHEET_QUERY_KEY]: _sheet, [COMPOSE_QUERY_KEY]: _compose, ...query } = route.query
  const back = router.options.history.state?.back
  if (back && back === router.resolve({ query }).fullPath) {
    router.back()
  } else {
    router.replace({ query })
  }
}

// 跨到桌面断点：关掉抽屉（去掉 query 与历史记录），已加载的回复回到第一页内联展示
watch(mobileShell, (mobile) => {
  if (mobile) return
  // 输入抽屉随移动端组件卸载，它的 query 在这里一并去掉
  if (route.query[COMPOSE_QUERY_KEY]) {
    leaveOverlayRoutes()
  } else {
    closeReplySheet()
  }
  Object.values(repliesData.value).forEach((data) => {
    if (data.pages[0]) data.currentPage = 0
  })
})

// 抽屉「加载更多」：从最后一个已加载页继续向后拉（抽屉拼接展示全部已加载页，不依赖 currentPage）
const loadMoreReplies = (commentId) => {
  const data = getRepliesData(commentId)
  if (data.loading || data.pageLoading) return
  data.currentPage = data.pages.length - 1
  return nextPage(commentId)
}

// 评论 / 回复点赞：按评论 id 各自防抖串行提交期望状态（显式 action，幂等可重试），
// 连续点赞多条评论互不覆盖
const likeToggle = useInteractionToggle({
  request: async (key, desired) => {
    const [type, targetId] = key.split(':')
    const res = await toggleLike({ type, target_id: targetId, action: desired ? 'like' : 'unlike' })
    return res.data.is_liked
  }
})

const setTargetLiked = (target, liked) => {
  if (target.liked === liked) return
  target.liked = liked
  target.like_count = liked ? target.like_count + 1 : target.like_count - 1
}

const handleToggleLike = (type, target) => {
  // 访客点赞前置拦截：弹登录引导，不触发 401 硬跳转
  if (!auth.isAuthenticated()) {
    requireLogin('like')
    return
  }
  setTargetLiked(target, !target.liked)
  likeToggle.schedule(`${type}:${target.id}`, {
    get: () => target.liked,
    apply: (liked) => setTargetLiked(target, liked),
    onError: (error) => message.error(interactionErrorMessage(error, t))
  })
}

const handleReplySubmit = async (parentComment, newReply) => {
  activeReplyId.value = null

  // 移动端回复区在抽屉里：抽屉还没加载过时只加回复数，打开抽屉时再完整拉取
  // （否则缓存里只有这一条新回复，打开抽屉会被当成已加载而不再请求）
  if (mobileShell.value && !isRepliesExpanded(parentComment.id)) {
    parentComment.reply_count = (parentComment.reply_count || 0) + 1
    return
  }

  // 如果有新回复的数据，直接添加到前端列表
  if (newReply) {
    const data = getRepliesData(parentComment.id)

    // 确保第一页已加载
    if (!data.pages[0]) {
      data.pages[0] = []
    }

    // 将新回复添加到第一页的开头
    data.pages[0].unshift(newReply)

    // 更新父评论的回复计数
    parentComment.reply_count = (parentComment.reply_count || 0) + 1

    // 如果当前不在第一页，切换回第一页
    if (data.currentPage !== 0) {
      data.currentPage = 0
    }

    // 如果回复区域未展开，展开它
    if (!isRepliesExpanded(parentComment.id)) {
      expandReplies(parentComment.id)
    }
  } else {
    // 降级处理：如果没有返回数据，则重新加载
    resetRepliesData(parentComment.id)
    if (isRepliesExpanded(parentComment.id)) {
      await loadReplies(parentComment)
    } else if (parentComment.reply_count === 0) {
      await loadReplies(parentComment)
    }
  }
}

// 输入抽屉发送成功：插入列表（沿用桌面的乐观插入），再滚动到并高亮新评论
// （主列表里回复未加载过的根评论只加回复数，新回复不在页面上，不滚动）
const handleComposerSubmit = async (created, target) => {
  if (!target.rootId) {
    if (created) addComment(created)
    else refreshComments()
  } else {
    const root = comments.value.find(c => c.id === target.rootId)
    if (root) await handleReplySubmit(root, created)
  }
  if (!created?.id) return
  highlightCommentId.value = created.id
  pendingComposerScrollId = created.id
}

// 输入抽屉离场动画结束后再滚到新评论：动画期间页面滚动仍被锁住，
// 且 router.back 关抽屉时浏览器会恢复打开前的滚动位置，提前滚会被覆盖
let pendingComposerScrollId = ''
const handleComposerClosed = () => {
  if (!pendingComposerScrollId) return
  scrollToComment(pendingComposerScrollId)
  pendingComposerScrollId = ''
}

// 移动端底部输入条点评论数：滚动到评论区
const scrollToList = () => {
  const el = commentListCardRef.value?.$el || commentListCardRef.value
  el?.scrollIntoView({ behavior: 'smooth', block: 'start' })
}

// 添加新评论到列表顶部
const addComment = (newComment) => {
  if (!newComment) return

  // 将新评论添加到列表顶部
  comments.value.unshift(newComment)

  // 更新评论总数
  emit('update:commentCount', (props.commentCount || 0) + 1)
}

// 无限滚动
const loadMoreTrigger = ref(null)
let observer = null

// 懒加载相关
const commentListCardRef = ref(null)
const commentsLoaded = ref(false)
let lazyObserver = null

// 排序映射：newest → 1（时间倒序），hottest → 0（点赞倒序）
const getSortValue = () => props.sort === 'newest' ? 1 : 0

// 回复排序值：0=最热（最多点赞），1=最新（默认）；按评论独立存储
const getRepliesSortValue = (commentId) => getRepliesData(commentId).sort

// 切换回复排序
const toggleRepliesSort = async (commentId) => {
  const data = getRepliesData(commentId)

  // 如果正在加载，忽略
  if (data.sortLoading) return

  // 切换该评论自己的排序值（不影响其他评论）
  data.sort = data.sort === 0 ? 1 : 0

  // 设置排序加载状态
  data.sortLoading = true

  try {
    // 重新加载第一页（使用新的排序值）
    const comment = comments.value.find(c => c.id === commentId)
    if (comment) {
      const params = {
        root_id: comment.id,
        sort: getRepliesSortValue(commentId)
      }

      const res = await getCommentReplies(params)
      if (res.data) {
        const items = res.data.items || []
        // 回灌提及用户（后端 mentions 字段）后再进响应式数据
        seedContentMentions(items)

        // 加载成功后，替换数据为第一页
        data.pages = [items]
        data.currentPage = 0
        data.cursors = ['', res.data.next_cursor || '']
        data.hasMoreMap = { 0: items.length > 0 && res.data.has_more }
      }
    }
  } catch (error) {
    console.error('切换排序失败:', error)
  } finally {
    data.sortLoading = false
  }
}

// 加载评论列表
const loadComments = async (isRefresh = false) => {
  if (loading.value) return
  if (!isRefresh && !hasMore.value && initialized.value) return

  loading.value = true
  try {
    const params = {
      post_id: props.postId,
      sort: getSortValue()
    }
    if (!isRefresh && nextCursor.value) {
      params.cursor = nextCursor.value
    }

    const res = await getCommentList(params)
    if (res.data) {
      // 回灌本页评论的提及用户（后端 mentions 字段），渲染前就位
      seedContentMentions(res.data.items || [])
      if (isRefresh) {
        comments.value = res.data.items || []
      } else {
        comments.value.push(...(res.data.items || []))
      }
      hasMore.value = res.data.has_more
      nextCursor.value = res.data.next_cursor || ''
    }
  } catch (error) {
    console.error('加载评论失败:', error)
    // 帖子已不可见（删除 / 私密圈被移出）：停止翻页，避免触底哨兵反复重试
    if (isCommentUnavailableError(error)) hasMore.value = false
  } finally {
    loading.value = false
    initialized.value = true

    // 评论加载完成后，设置无限滚动 observer
    if (hasMore.value) {
      await nextTick()
      setupInfiniteScrollObserver()
    }
  }
}

// ============ 通知点击定位评论 ============

// 定位加载顶层列表：以 locate 返回的游标拉一页作为评论流第 0 页
// （之前的页不可达，只支持向后翻——对接文档约定）
// sortValue 须用定位流程捕获的排序：list_cursor 仅在生成它的 sort 下有效
const loadLocatedComments = async (cursor, sortValue, isStale) => {
  loading.value = true
  try {
    const params = {
      post_id: props.postId,
      sort: sortValue
    }
    if (cursor) {
      params.cursor = cursor
    }

    const res = await getCommentList(params)
    // 流程已被更新的定位目标取代：丢弃结果，不回灌评论流
    if (isStale?.()) return
    if (res.data) {
      seedContentMentions(res.data.items || [])
      comments.value = res.data.items || []
      hasMore.value = res.data.has_more
      nextCursor.value = res.data.next_cursor || ''
    }
  } finally {
    loading.value = false
    initialized.value = true

    if (hasMore.value) {
      await nextTick()
      setupInfiniteScrollObserver()
    }
  }
}

// 定位加载回复：以 locate 返回的游标拉一页，落到 reply_page 对应页码并展开
const loadLocatedReplies = async (comment, cursor, replyPage) => {
  const data = getRepliesData(comment.id)
  loadingReplies.value[comment.id] = true

  try {
    const params = {
      root_id: comment.id,
      sort: getRepliesSortValue(comment.id)
    }
    if (cursor) {
      params.cursor = cursor
    }

    const res = await getCommentReplies(params)
    if (res.data) {
      const items = res.data.items || []
      seedContentMentions(items)

      // 定位页直接落在 reply_page（从1开始）；之前的页不加载，prevPage 对空页有守卫
      const pageIndex = Math.max(0, (replyPage || 1) - 1)
      data.pages = new Array(pageIndex + 1).fill(null)
      data.pages[pageIndex] = items
      data.currentPage = pageIndex
      data.cursors = new Array(pageIndex + 2).fill('')
      data.cursors[pageIndex] = cursor || ''
      data.cursors[pageIndex + 1] = res.data.next_cursor || ''
      data.hasMoreMap = { [pageIndex]: res.data.has_more }

      expandReplies(comment.id)
    }
  } catch (error) {
    console.error('定位加载回复失败:', error)
  } finally {
    loadingReplies.value[comment.id] = false
  }
}

// 回复定位：加载含目标的回复页 + 漂移兜底（当页没有则翻下一页再找一次）
const locateReply = async (root, located) => {
  await loadLocatedReplies(root, located.reply_cursor, located.reply_page)
  if (getCurrentReplies(root.id).some(r => r.id === located.comment_id)) return true
  if (!hasNextPage(root.id)) return false
  await nextPage(root.id)
  return getCurrentReplies(root.id).some(r => r.id === located.comment_id)
}

// 定位主流程：locate → 拉含根评论的页 →（回复时）展开回复 → 滚动 + 高亮
const locateAndJump = async (commentId) => {
  const version = ++locateVersion
  if (locating.value) {
    // 已有定位流程在飞：作废旧流程，记录最新目标，等其 settle 后补跑
    pendingLocateId = commentId
    return
  }
  locating.value = true
  highlightCommentId.value = ''
  const isStale = () => version !== locateVersion

  try {
    // 首个 await 前捕获排序：定位流程内所有请求（含 list_cursor 请求）复用同一 sort，
    // 防止 props.sort 中途变化后与旧 sort 下算出的游标混用
    const sortValue = getSortValue()
    const res = await locateComment({
      comment_id: commentId,
      sort: sortValue,
      reply_sort: 1 // 回复列表默认最新，与 getRepliesData().sort 初始值一致
    })
    if (isStale()) return
    const d = res.data
    if (!d) return

    // 防串帖：目标评论不属于当前帖子
    if (d.post_id && d.post_id !== props.postId) {
      message.error(t('notice.locateFailed'))
      return
    }

    await loadLocatedComments(d.list_cursor, sortValue, isStale)
    if (isStale()) return

    // 并发漂移兜底：目标根评论不在定位页则再向后拉一页（对接文档 §四-1）
    let root = comments.value.find(c => c.id === d.root_id)
    if (!root && hasMore.value) {
      await loadComments()
      if (isStale()) return
      root = comments.value.find(c => c.id === d.root_id)
    }
    if (!root) {
      message.error(t('notice.locateFailed'))
      return
    }

    let targetId = d.root_id
    if (!d.is_root) {
      const found = await locateReply(root, d)
      if (isStale()) return
      if (!found) {
        message.error(t('notice.locateFailed'))
        return
      }
      targetId = d.comment_id
    }

    highlightCommentId.value = targetId
    await nextTick()
    if (isStale()) return

    // 移动端回复在抽屉里：主列表先定位到根评论（关抽屉后停在上下文处），再打开抽屉滚到目标回复
    if (mobileShell.value && !d.is_root) {
      scrollToComment(root.id, { behavior: 'auto' })
      // 防御：抽屉已开着该根评论时 after-enter 不会再触发，直接滚
      if (sheetComment.value?.id === root.id) {
        scrollToComment(targetId)
      } else {
        pendingSheetScrollId = targetId
        openReplySheet(root, { fromLocate: true })
      }
      return
    }

    scrollToComment(targetId)
  } catch (error) {
    if (isStale()) return // 过期目标的错误不提示，交由最新流程处理
    // 业务码 40401 = 评论不存在、已删除或因审核不可见（对接文档 §3.2）；私密圈不可见时为 404
    if (isCommentUnavailableError(error)) {
      message.info(t('notice.targetDeleted'))
    } else {
      console.error('定位评论失败:', error)
      message.error(t('notice.locateFailed'))
    }
  } finally {
    locating.value = false
    // 在飞期间有新目标到来：settle 后补跑最新一个
    if (pendingLocateId) {
      const next = pendingLocateId
      pendingLocateId = ''
      locateAndJump(next)
    }
  }
}

// 加载子回复（游标分页）
const loadReplies = async (comment) => {
  const data = getRepliesData(comment.id)

  // 如果正在加载，直接返回
  if (data.loading) return

  data.loading = true
  loadingReplies.value[comment.id] = true

  try {
    const params = {
      root_id: comment.id,
      sort: getRepliesSortValue(comment.id)
    }

    // 非首页需要传游标
    if (data.currentPage > 0 && data.cursors[data.currentPage]) {
      params.cursor = data.cursors[data.currentPage]
    }

    const res = await getCommentReplies(params)
    if (res.data) {
      const items = res.data.items || []
      // 回灌提及用户（后端 mentions 字段）后再进响应式数据
      seedContentMentions(items)

      // 保存当前页数据
      data.pages[data.currentPage] = items
      data.cursors[data.currentPage + 1] = res.data.next_cursor || ''
      data.hasMoreMap[data.currentPage] = res.data.has_more

      // 展开回复区域
      expandReplies(comment.id)
    }
  } catch (error) {
    console.error('加载回复失败:', error)
  } finally {
    data.loading = false
    loadingReplies.value[comment.id] = false
  }
}

// 上一页（从缓存中读取）
const prevPage = (commentId) => {
  const data = getRepliesData(commentId)
  if (data.currentPage > 0) {
    data.currentPage = data.currentPage - 1
  }
}

// 下一页（可能需要加载）
const nextPage = async (commentId) => {
  const comment = comments.value.find(c => c.id === commentId)
  if (!comment) return

  const data = getRepliesData(commentId)

  // 如果正在加载，忽略
  if (data.pageLoading) return

  // 如果下一页已缓存，直接切换
  if (data.pages[data.currentPage + 1]) {
    data.currentPage = data.currentPage + 1
    return
  }

  // 如果当前页有更多数据，加载下一页
  if (data.hasMoreMap[data.currentPage] && !data.loading) {
    data.pageLoading = true
    loadingReplies.value[commentId] = true

    try {
      const params = {
        root_id: comment.id,
        sort: getRepliesSortValue(commentId),
        cursor: data.cursors[data.currentPage + 1]
      }

      const res = await getCommentReplies(params)
      if (res.data) {
        const items = res.data.items || []
        // 回灌提及用户（后端 mentions 字段）后再进响应式数据
        seedContentMentions(items)

        // 保存到下一页
        const nextPageIndex = data.currentPage + 1
        data.pages[nextPageIndex] = items
        data.cursors[nextPageIndex + 1] = res.data.next_cursor || ''
        data.hasMoreMap[nextPageIndex] = res.data.has_more

        // 加载成功后才切换页面
        data.currentPage = nextPageIndex
      }
    } catch (error) {
      console.error('加载回复失败:', error)
    } finally {
      data.pageLoading = false
      loadingReplies.value[commentId] = false
    }
  }
}

// 刷新评论（供父组件调用）
const refreshComments = () => {
  // 清理 observer
  if (observer) {
    observer.disconnect()
    observer = null
  }

  // 重置状态
  comments.value = []
  hasMore.value = false
  nextCursor.value = ''
  repliesData.value = {}
  expandedReplyIds.value = []
  commentsLoaded.value = false
  highlightCommentId.value = ''

  // 重新设置懒加载（如果已经在视口内会立即触发）
  nextTick(() => {
    setupLazyObserver()
  })
}

// 监听排序变化
watch(() => props.sort, () => {
  refreshComments()
})

// 懒加载 Observer - 监听评论区容器进入视口
const setupLazyObserver = () => {
  if (!commentListCardRef.value || commentsLoaded.value) return

  lazyObserver = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting && !commentsLoaded.value) {
          commentsLoaded.value = true
          // 先清理可能存在的旧 observer
          if (observer) {
            observer.disconnect()
            observer = null
          }
          loadComments(true)
        }
      })
    },
    {
      rootMargin: '200px',
      threshold: 0.01
    }
  )

  lazyObserver.observe(commentListCardRef.value.$el || commentListCardRef.value)
}

// 无限滚动 Observer - 监听滚动到底部
const setupInfiniteScrollObserver = () => {
  if (!loadMoreTrigger.value) return

  // 先清理已存在的 observer
  if (observer) {
    observer.disconnect()
    observer = null
  }

  observer = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting && hasMore.value && !loading.value) {
          loadComments()
        }
      })
    },
    {
      rootMargin: '100px',
      threshold: 0.1
    }
  )

  observer.observe(loadMoreTrigger.value)
}

const cleanupObserver = () => {
  if (observer) {
    observer.disconnect()
    observer = null
  }
  if (lazyObserver) {
    lazyObserver.disconnect()
    lazyObserver = null
  }
}

onMounted(() => {
  // 刷新或从其它页后退回来时评论流已重建：残留的抽屉 query 不再对应已加载数据，直接去掉
  if (route.query[SHEET_QUERY_KEY] || route.query[COMPOSE_QUERY_KEY]) {
    leaveOverlayRoutes()
  }
  // 带定位参数进帖：跳过懒加载直接定位（定位流程自己会拉评论流）
  if (props.locateCommentId) {
    commentsLoaded.value = true
    locateAndJump(props.locateCommentId)
  } else {
    setupLazyObserver()
  }
})

// 同帖内通知再点击（路由复用）：评论流重置后重新定位
watch(() => props.locateCommentId, (id, oldId) => {
  if (!id || id === oldId) return
  refreshComments()
  // refreshComments 会在 nextTick 重建懒加载 observer；抢先标记已加载使其空转
  commentsLoaded.value = true
  nextTick(() => locateAndJump(id))
})

onUnmounted(() => {
  cleanupObserver()
})

defineExpose({ refreshComments, addComment, openComposer, scrollToList })
</script>

<style scoped>
.comment-list-card {
  border-radius: 16px !important;
  scroll-margin-top: calc(var(--header-height) + 12px);
}

.comment-section-title {
  font-size: 18px;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.9);
}

.comment-list-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 16px;
}

.comment-sort {
  display: flex;
  align-items: center;
  gap: 4px;
}

.comment-list {
  display: flex;
  flex-direction: column;
}

/* 查看回复按钮 */
.load-replies-btn {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  margin-top: 8px;
  padding: 4px 0;
  font-size: 13px;
  color: #63e2b7;
  cursor: pointer;
  user-select: none;
}

@media (hover: hover) {
  .load-replies-btn:hover {
    color: #7fe7c4;
  }
}

/* 无限滚动触发器 */
.load-more-trigger {
  height: 1px;
  margin-top: 20px;
}

/* 加载状态 */
.loading-state {
  display: flex;
  justify-content: center;
  padding: 32px 0;
}

/* 没有更多 */
.no-more {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px 0;
  color: rgba(255, 255, 255, 0.4);
  font-size: 13px;
}

.no-more::before,
.no-more::after {
  content: '';
  flex: 1;
  height: 1px;
  background: rgba(255, 255, 255, 0.1);
  margin: 0 20px;
}

:deep(.md-editor-dark) {
  --md-bk-color: rgb(24, 24, 28);
  --md-scrollbar-bg-color: rgb(24, 24, 28);
}

:deep(.md-editor-dark .md-editor-preview) {
  --md-theme-bg-color: rgb(24, 24, 28);
}

/* 移动端回复抽屉：抽屉容器样式在 main.css（.reply-sheet），以下是本模板内元素 */
.reply-sheet-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
}

.reply-sheet-title {
  font-size: 16px;
  font-weight: 600;
  color: var(--text-primary);
}

.reply-sheet-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  margin-right: -8px;
  padding: 0;
  background: transparent;
  border: none;
  border-radius: 50%;
  color: var(--text-tertiary);
  cursor: pointer;
}

.reply-sheet-close:active {
  background: rgba(255, 255, 255, 0.08);
}

/* 根评论作为上下文，与下方回复列表分隔 */
.comment-item.reply-sheet-root {
  padding-top: 4px;
  margin-bottom: 8px;
}

:deep(.n-image-preview){
  z-index: 9999 !important;
}
</style>

