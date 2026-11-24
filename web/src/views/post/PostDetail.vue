<template>
  <div class="post-detail-page">
    <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell） -->
    <AppShell v-model:offset="offset" />

    <!-- 主内容区域 -->
    <div class="main-content" :style="[contentStyle, composeBarPadding]">
      <div v-if="loading" class="loading-container">
        <NSpin size="large" />
      </div>

      <div v-else-if="post" class="post-detail-container">
        <!-- 左侧内容列 -->
        <div class="post-main-column">
            <PostHeaderAndContent
              :post="post"
              :post-id="route.params.id"
              :language="language"
              :is-author="isAuthor"
              :review-slow="reviewSlow"
              :refreshing="reviewRefreshing"
              @like="handleLike"
              @collect="handleCollect"
              @refresh="refreshReview"
            >
            <!-- 未发布（审核中 / 驳回 / 屏蔽）的帖子后端不接受评论，评论区整体不渲染 -->
            <template v-if="published">
            <!-- 移动端门户：顶层评论走底部输入条 + 输入抽屉（见下方 MobileComposeBar 与 docs/mobile-adaptation-plan.md 6.6） -->
            <CommentEditor
              v-if="!mobileShell"
              :post-id="route.params.id"
              :circle-id="post.circle_id"
              :language="language"
              @submit="handleSubmitComment"
              @upload-img="handleCommentUploadImg"
            />

            <CommentList
              ref="commentListRef"
              :post-id="route.params.id"
              :circle-id="post.circle_id"
              v-model:sort="commentSort"
              v-model:comment-count="commentCount"
              :language="language"
              :locate-comment-id="route.query.comment_id || ''"
            />
            </template>
          </PostHeaderAndContent>
        </div>

        <!-- 右侧圈子信息卡片 -->
        <div class="right-sidebar">
          <CircleInfoCard
            v-if="post && post.circle_id"
            :circleId="post.circle_id"
          />
        </div>
      </div>

      <!-- 不存在 / 非作者打开未发布的帖子（详情 404）：中性空态，不透露帖子是否在审核 -->
      <NEmpty v-else :description="t('post.unavailable')" >
        <template #icon>
          <NIcon>
            <ArticleRound/>
          </NIcon>
        </template>
      </NEmpty> 
    </div>

    <!-- 移动端底部常驻「说点什么…」输入条：点击打开输入抽屉；未发布的帖子不能评论，不显示 -->
    <MobileComposeBar
      v-if="showComposeBar"
      fixed
      :placeholder="t('mobile.comment.barPlaceholder')"
      :count="commentCount"
      @compose="openTopLevelComposer"
      @count-click="commentListRef?.scrollToList()"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted, inject, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NSpin, NEmpty, useMessage, NIcon } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import AppShell from '@/components/layout/AppShell.vue'
import { useAppShell } from '@/composables/useAppShell'
import CircleInfoCard from '@/components/circle/CircleInfoCard.vue'
import PostHeaderAndContent from '@/components/post/detail/PostHeaderAndContent.vue'
import CommentEditor from '@/components/post/detail/CommentEditor.vue'
import MobileComposeBar from '@/components/post/detail/MobileComposeBar.vue'
import CommentList from '@/components/post/detail/CommentList.vue'
import { getPostDetail } from '@/api/post'
import { usePageTitle } from '@/composables/usePageTitle'
import { useInteractionToggle, interactionErrorMessage } from '@/composables/useInteractionToggle'
import { toggleLike } from '@/api/like'
import { toggleCollect } from '@/api/collect'
import { getCircleDetail } from '@/api/circle'
import { getUserInfo } from '@/api/auth'
import { useReviewPolling, isPostPublished, POST_STATUS } from '@/composables/usePostReview'
import { auth } from '@/utils/auth'
import { requireLogin } from '@/utils/guest-action'
import { seedContentMentions } from '@/utils/mentionResolve'
import {ArticleRound} from '@vicons/material'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const { t } = useI18n()
const { setTitleData } = usePageTitle()

// 注入圈子搜索状态设置方法
const setCircleSearch = inject('setCircleSearch', () => {})

const loading = ref(true)
const post = ref(null)
const language = ref('zh-CN')
// 侧栏宽度与内容区偏移（移动端外框下不偏移），见 composables/useAppShell.js
const { offset, mobileShell, contentStyle } = useAppShell()
// 未发布的帖子只有作者能看到，提示条 / 轮询也只对作者生效
const isAuthor = ref(false)
const published = computed(() => isPostPublished(post.value))

// 详情 404（信封 code=204）：帖子不存在，或非作者访问未发布的帖子，两者对外不作区分
const isNotFoundError = (error) => error?.status === 404 || error?.code === 204

// 当前用户 id 只在帖子未发布时才需要（判断作者），按页缓存
let currentUserId = null
const resolveIsAuthor = async (detail) => {
  if (isPostPublished(detail) || !auth.isAuthenticated()) return false
  if (!currentUserId) {
    try {
      const res = await getUserInfo()
      currentUserId = res.data?.id || null
    } catch (error) {
      console.error('获取用户信息失败:', error)
    }
  }
  // 拿不到当前用户时按后端约定兜底：未发布的帖子只会返回给作者本人
  return currentUserId ? currentUserId === detail.author_id : true
}

const applyPost = (detail) => {
  // 先回灌后端回传的提及用户，再进响应式渲染 → 链接化零搜索精确建链
  seedContentMentions([detail])
  post.value = detail
  // 设置评论数
  commentCount.value = detail.comment_count || 0

  // 数据加载成功后用帖子标题覆盖标签页标题
  if (detail.title) {
    setTitleData('title.postDetailName', { name: detail.title })
  }
}

// 发帖页跳转带 ?justCreated=1：取出并从地址栏去掉，刷新页面不再重复提示
const consumeJustCreated = () => {
  if (route.query.justCreated !== '1') return false
  const { justCreated, ...query } = route.query
  router.replace({ query })
  return true
}

// 发帖后按帖子的实际状态提示（驳回 / 屏蔽由提示条说明，不再弹 toast）
const notifyCreated = (status) => {
  if (status === POST_STATUS.PUBLISHED) {
    message.success(t('post.publishSuccess'))
  } else if (status === POST_STATUS.REVIEWING) {
    message.info(t('post.submittedForReview'))
  }
}

// 审核中轮询：每次只在状态变化时替换帖子数据，避免正文重复渲染
const refreshPostStatus = async () => {
  const postId = route.params.id
  try {
    const res = await getPostDetail(postId)
    if (postId !== route.params.id || !res.data) return null
    if (res.data.status !== post.value?.status) applyPost(res.data)
    return res.data.status
  } catch (error) {
    // 作者本人也拿到 404：帖子已被删除，转空态并停止轮询
    if (isNotFoundError(error) && postId === route.params.id) {
      post.value = null
      return null
    }
    throw error
  }
}

const reviewPoll = useReviewPolling({
  fetchStatus: refreshPostStatus,
  onSettled: (status) => {
    if (status === POST_STATUS.PUBLISHED) {
      message.success(t('post.reviewing.published'))
    }
  }
})
const { exhausted: reviewSlow, refreshing: reviewRefreshing, refresh: refreshReview } = reviewPoll

// 加载帖子详情
const loadPostDetail = async () => {
  const postId = route.params.id
  const justCreated = consumeJustCreated()
  reviewPoll.stop()
  loading.value = true
  post.value = null
  isAuthor.value = false
  try {
    const res = await getPostDetail(postId)
    if (postId !== route.params.id) return

    if (res.data) {
      const author = await resolveIsAuthor(res.data)
      if (postId !== route.params.id) return
      isAuthor.value = author
      applyPost(res.data)
      if (justCreated) notifyCreated(res.data.status)
      if (author && res.data.status === POST_STATUS.REVIEWING) {
        reviewPoll.start()
      }

      // 设置圈子搜索状态
      if (post.value.circle_id) {
        try {
          const circleRes = await getCircleDetail(post.value.circle_id)
          if (circleRes.data) {
            setCircleSearch({
              id: circleRes.data.id,
              name: circleRes.data.name,
              avatar_url: circleRes.data.avatar_url
            })
          }
        } catch (error) {
          console.error('获取圈子信息失败:', error)
        }
      }
    }
  } catch (error) {
    if (postId !== route.params.id) return
    console.error('加载帖子详情失败:', error)
    // 404 只显示空态，不弹带后端英文 message 的 toast
    if (!isNotFoundError(error)) {
      message.error(t('messages.getDetailFailed', { error: error.message }))
    }
  } finally {
    if (postId === route.params.id) loading.value = false
  }
}

// 点赞 / 收藏：乐观更新 UI，按帖子 id 防抖串行提交期望状态（显式 action，幂等可重试）
// ctx 捕获点击时的帖子对象，避免防抖期间切换到其它帖子后打错目标
const likeToggle = useInteractionToggle({
  request: async (postId, desired) => {
    const res = await toggleLike({ type: 'post', target_id: postId, action: desired ? 'like' : 'unlike' })
    return res.data.is_liked
  }
})

const collectToggle = useInteractionToggle({
  request: async (postId, desired) => {
    const res = await toggleCollect({ post_id: postId, action: desired ? 'collect' : 'uncollect' })
    return res.data.is_collected
  }
})

const notifyInteractionError = (error) => {
  message.error(interactionErrorMessage(error, t))
}

const setPostLiked = (target, liked) => {
  if (target.is_liked === liked) return
  target.is_liked = liked
  target.like_count = liked ? target.like_count + 1 : target.like_count - 1
}

const handleLike = () => {
  if (!published.value) return
  // 访客点赞前置拦截：避免触发 401 硬跳转，改为弹登录引导
  if (!auth.isAuthenticated()) {
    requireLogin('like')
    return
  }
  const target = post.value
  setPostLiked(target, !target.is_liked)
  likeToggle.schedule(target.id, {
    get: () => target.is_liked,
    apply: (liked) => setPostLiked(target, liked),
    onError: notifyInteractionError
  })
}

// 收藏数 collect_count 由后端异步聚合，前端不本地 ±1（对接文档约定）
const handleCollect = () => {
  if (!published.value) return
  // 访客收藏前置拦截
  if (!auth.isAuthenticated()) {
    requireLogin('collect')
    return
  }
  const target = post.value
  target.is_collected = !target.is_collected
  collectToggle.schedule(target.id, {
    get: () => target.is_collected,
    apply: (collected) => { target.is_collected = collected },
    onError: notifyInteractionError
  })
}

// ============ 评论区相关 ============
const commentSort = ref('hottest')
const commentListRef = ref(null)
const commentCount = ref(0) // 独立的评论数变量

// 提交评论（API 调用已在 CommentEditor 内部完成，此处只做后续处理）
const handleSubmitComment = (newComment) => {
  // 直接将新评论添加到列表中，而不是刷新整个列表
  if (newComment) {
    commentListRef.value?.addComment(newComment)
  } else {
    // 降级处理：如果没有返回数据，则刷新列表
    commentListRef.value?.refreshComments()
  }
}

// 移动端底部输入条：只在评论区渲染时显示（已发布）；内容区底部让出输入条高度，最后一条评论不被挡住
const showComposeBar = computed(() => mobileShell.value && !loading.value && published.value)
const composeBarPadding = computed(() => (showComposeBar.value
  ? { 'padding-bottom': 'calc(var(--compose-bar-height) + var(--safe-bottom) + 16px)' }
  : null))

// 须在点击处理里同步打开（iOS 弹键盘依赖用户手势），访客由输入抽屉弹登录引导
const openTopLevelComposer = () => {
  commentListRef.value?.openComposer({ rootId: null, replyToId: null, replyToName: '' })
}

// 评论图片上传
const handleCommentUploadImg = async (files, callback) => {
  // TODO: 对接图片上传API
  const urls = await Promise.all(
    files.map(() => Promise.resolve('https://via.placeholder.com/300'))
  )
  callback(urls)
}

onMounted(() => {
  loadPostDetail()
})

// 路由以 route.name 为 key，帖子之间跳转（如通知 / 提及链接）复用本组件：按 id 重新加载，旧帖子的轮询随之停止。
// 离开详情页时 route 先切到目标页（其 params.id 可能是用户 / 圈子 id），按路由名过滤
watch(() => route.params.id, (id, oldId) => {
  if (route.name !== 'post-detail' || !id || id === oldId) return
  loadPostDetail()
})
</script>

<style scoped>
.post-detail-page {
  min-height: 100vh;
  position: relative;
}

.main-content {
  margin-top: var(--header-height);
  min-height: calc(100vh - var(--header-height));
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.post-detail-container {
  display: flex;
  gap: 24px;
  width: 100%;
  align-items: flex-start;
  justify-content: center;
}

.loading-container {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 400px;
}

.post-main-column {
  flex: 1;
  max-width: 53dvw;
  min-width: 40dvw;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

/* 右侧悬浮栏 */
.right-sidebar {
  width: 23dvw;
  flex-shrink: 0;
  position: sticky;
  top: calc(var(--header-height) + 24px);
  max-height: calc(100vh - var(--header-height) - 24px);
  max-height: calc(100dvh - var(--header-height) - 24px);
  overflow-y: auto;
  /* 隐藏滚动条 */
  scrollbar-width: none; /* Firefox */
  -ms-overflow-style: none; /* IE and Edge */
}

.right-sidebar::-webkit-scrollbar {
  display: none; /* Chrome, Safari, Opera */
}

/* 响应式 */
@media (max-width: 1400px) {
  .main-content {
    margin-right: 24px;
  }

  .right-sidebar {
    width: 320px;
  }
}

@media (max-width: 1200px) {
  .right-sidebar {
    display: none;
  }

  .main-content {
    margin-right: 24px;
    margin-left: 80px;
  }
}

@media (max-width: 768px) {
  .main-content {
    margin-left: 0;
    margin-right: 0;
    padding: 12px;
  }

  .post-detail-container {
    max-width: 100%;
  }

  /* 桌面的 dvw 区间在手机上会把正文压成窄条，移动端占满 */
  .post-main-column {
    max-width: none;
    min-width: 0;
    width: 100%;
    gap: 12px;
  }
}
</style>
