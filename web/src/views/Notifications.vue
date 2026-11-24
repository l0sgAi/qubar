<template>
  <div class="notifications-page">
    <!-- 顶栏 + 侧边栏（移动端外框切换见 AppShell） -->
    <AppShell v-model:offset="offset" />

    <!-- 主内容区域 -->
    <div class="content-wrapper" :style="contentStyle">
      <div class="main-content">
        <div class="notice-container">
          <!-- 页头 -->
          <div class="notice-header">
            <h1 class="page-title">{{ t('notice.title') }}</h1>
            <NButton
              v-if="notices.length"
              quaternary
              size="small"
              class="read-all-btn"
              :loading="readingAll"
              @click="handleReadAll"
            >
              {{ t('notice.readAll') }}
            </NButton>
          </div>

          <!-- 类型过滤：顶栏 Tab -->
          <div class="notice-tabs">
            <button
              v-for="tab in tabs"
              :key="tab.type"
              class="notice-tab"
              :class="{ active: activeType === tab.type }"
              @click="switchTab(tab.type)"
            >
              {{ tab.label }}
            </button>
          </div>

          <!-- 通知列表：整条为真实链接，右键/中键可新标签页打开 -->
          <div class="notice-list">
            <SmartLink
              v-for="item in notices"
              :key="item.id"
              class="notice-item"
              :class="{ unread: !item.is_read, inert: isInert(item) }"
              :to="noticeTarget(item)"
              v-bind="lookupAttrs(item)"
              @click="handleItemClick(item)"
              @keydown.enter="needsCircleLookup(item) && handleItemClick(item)"
            >
              <!-- 系统类通知（审核结果、举报、下架）无 actor：用系统图标代替首字母兜底 -->
              <NAvatar
                v-if="isSystemNotice(item)"
                round
                :size="40"
                class="notice-avatar system-avatar"
              >
                <NSpin v-if="resolvingId === item.id" :size="18" />
                <NIcon v-else :size="22" :component="systemIcon(item)" />
              </NAvatar>
              <NAvatar
                v-else
                round
                :size="40"
                :src="item.actor?.avatar_url || undefined"
                class="notice-avatar"
              >
                <!-- NAvatar 的 default slot 优先级高于 src：兜底字母必须 v-if，
                     否则图片永远不会渲染（对齐 CommentList 写法） -->
                <div v-if="!item.actor?.avatar_url">{{ (item.actor?.username || t('notice.someone')).charAt(0).toUpperCase() }}</div>
              </NAvatar>
              <div class="notice-body">
                <div class="notice-text">{{ renderText(item) }}</div>
                <div class="notice-time">{{ formatTime(item.create_time) }}</div>
              </div>
              <!-- 被关注通知：直接回关（关系由 POST /follow/relations 批量取，取到前不显示） -->
              <FollowButton
                v-if="followRelationOf(item) !== null"
                class="notice-follow-btn"
                :user-id="followNoticeActorId(item)"
                :relation="followRelationOf(item)"
                @update:relation="(v) => setFollowRelation(followNoticeActorId(item), v)" />
              <span v-if="!item.is_read" class="unread-dot"></span>
            </SmartLink>

            <!-- 空态 -->
            <NEmpty v-if="!loading && !notices.length" :description="t('notice.empty')" class="notice-empty" />

            <!-- 加载中 -->
            <div v-if="loading" class="notice-loading">
              <NSpin size="small" />
            </div>

            <!-- 无限滚动触发器 / 到底提示 -->
            <div v-if="hasMore" ref="loadMoreTrigger" class="load-more-trigger"></div>
            <div v-else-if="notices.length" class="no-more">{{ t('notice.noMore') }}</div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { NAvatar, NButton, NEmpty, NIcon, NSpin, useMessage } from 'naive-ui'
import { Flag, ShieldCheck, ShieldX } from '@vicons/tabler'
import { useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppShell from '@/components/layout/AppShell.vue'
import { useAppShell } from '@/composables/useAppShell'
import FollowButton from '@/components/follow/FollowButton.vue'
import { getFollowRelations } from '@/api/follow'
import SmartLink from '@/components/common/SmartLink.vue'
import { getNoticeList, markNoticesRead, markAllNoticesRead } from '@/api/notice'
import { getPostDetail } from '@/api/post'
import { getManagedCircles } from '@/api/circle'
import { useFormatTime } from '@/utils/i18n'
import {
  renderNoticeText, noticeTarget, isSystemNotice, needsCircleLookup, reportedQueueTarget, MANAGED_CIRCLES_TARGET,
  NOTICE_TYPE, REVIEW_NOTICE_TYPES, FOLLOW_NOTICE_TYPES, followNoticeActorId
} from '@/utils/noticeDisplay'

const message = useMessage()
const router = useRouter()
const { t } = useI18n()
const { formatTime } = useFormatTime()

// 侧栏宽度与内容区偏移（移动端外框下不偏移），见 composables/useAppShell.js
const { offset, contentStyle } = useAppShell()

// 类型过滤 Tab：type 对应后端 notice_type，空/0=全部
// 接口支持逗号分隔多值过滤（notice_type IN (...)），帖子被赞(1)与评论被赞(2)同属「点赞」tab
// 「审核」tab 含 7–11（作者侧审核结果 / 下架 + 圈管侧举报 / 自伤关注）；type 只接受数字，传 'review' 等字符串会 400
const tabs = computed(() => [
  { type: 0, label: t('notice.tabs.all') },
  { type: '1,2', label: t('notice.tabs.like') },
  { type: 3, label: t('notice.tabs.collect') },
  { type: 4, label: t('notice.tabs.comment') },
  { type: 5, label: t('notice.tabs.reply') },
  { type: 6, label: t('notice.tabs.mention') },
  { type: FOLLOW_NOTICE_TYPES, label: t('notice.tabs.follow') },
  { type: REVIEW_NOTICE_TYPES, label: t('notice.tabs.review') }
])
const activeType = ref(0)

const notices = ref([])
const loading = ref(false)
const cursor = ref('')
const hasMore = ref(true)
const readingAll = ref(false)

// 文案模板按类型渲染，见 utils/noticeDisplay.js
const renderText = (item) => renderNoticeText(item, t)

// 没有跳转目标的通知（如关注者已注销的类型 12）：不可点，去掉手型与悬浮高亮
const isInert = (item) => !noticeTarget(item) && !needsCircleLookup(item)

// 被关注通知的回关按钮：关注者 ID → 我与 TA 的关系（通知本身不带关系，按页批量查询）
const followRelations = ref({})
const followRelationOf = (item) => {
  const id = followNoticeActorId(item)
  if (!id) return null
  const rel = followRelations.value[id]
  return typeof rel === 'number' ? rel : null
}
const setFollowRelation = (userId, relation) => {
  followRelations.value = { ...followRelations.value, [userId]: relation }
}
const loadFollowRelations = async (items) => {
  const ids = items.map(followNoticeActorId).filter(id => id && !(id in followRelations.value))
  if (!ids.length) return
  try {
    const res = await getFollowRelations(ids)
    followRelations.value = { ...followRelations.value, ...(res?.data?.relations || {}) }
  } catch (error) {
    // 关系查不到只是不显示回关按钮，不打扰用户
    console.error('获取关注关系失败:', error)
  }
}

// 加载世代：切换过滤类型后旧请求的响应直接丢弃，不得覆盖新列表/游标
let loadGen = 0

// 加载通知列表（keyset 游标分页，只顺序向后翻）
const loadNotices = async (isRefresh = false) => {
  if (!isRefresh && (loading.value || !hasMore.value)) return

  const gen = ++loadGen
  loading.value = true
  let succeeded = false
  try {
    const params = { type: activeType.value, size: 20 }
    if (!isRefresh && cursor.value) {
      params.cursor = cursor.value
    }

    const res = await getNoticeList(params)
    if (gen !== loadGen) return // 已切换过滤类型，过期响应丢弃
    if (res.data) {
      const items = res.data.notices || []
      notices.value = isRefresh ? items : [...notices.value, ...items]
      loadFollowRelations(items)
      // 空字符串游标 = 没有更多
      cursor.value = res.data.cursor || ''
      hasMore.value = cursor.value !== ''
      succeeded = true
    }
  } catch (error) {
    if (gen !== loadGen) return
    console.error('加载通知失败:', error)
    message.error(t('notice.loadFailed'))
  } finally {
    // 过期请求的收尾不得动当前世代的 loading/observer
    if (gen === loadGen) {
      loading.value = false
      // 仅成功后才挂观察器：失败时触发器常驻视口会造成无限自动重试
      if (succeeded && hasMore.value) {
        await nextTick()
        setupObserver()
      }
    }
  }
}

// 切换类型过滤：重置游标重新拉
const switchTab = (type) => {
  if (type === activeType.value) return
  activeType.value = type
  notices.value = []
  cursor.value = ''
  hasMore.value = true
  cleanupObserver()
  loadNotices(true)
}

// 点击通知顺手标记已读并校正顶栏角标（跳转由 SmartLink 承担）
const markReadAndBump = async (item) => {
  if (item.is_read) return
  item.is_read = true
  try {
    await markNoticesRead([item.id])
    window.dispatchEvent(new CustomEvent('notice-read', { detail: { count: 1 } }))
  } catch (error) {
    console.error('标记已读失败:', error)
  }
}

// 系统通知图标：举报 → 旗标，驳回 / 下架 → 盾牌叉，通过 → 盾牌勾
const SYSTEM_ICONS = {
  [NOTICE_TYPE.POST_REPORTED]: Flag,
  [NOTICE_TYPE.POST_REJECTED]: ShieldX,
  [NOTICE_TYPE.POST_REMOVED]: ShieldX
}
const systemIcon = (item) => SYSTEM_ICONS[item.notice_type] || ShieldCheck

// 类型 9 缺 circle_id 时 SmartLink 降级为 div：补上链接语义，键盘可聚焦、回车触发
const lookupAttrs = (item) => (needsCircleLookup(item) ? { role: 'link', tabindex: 0 } : {})

const handleItemClick = (item) => {
  markReadAndBump(item)
  if (needsCircleLookup(item)) openReportedNotice(item)
}

// 举报通知兜底跳转（后端 l0sgAi/qubar#156 上线前列表项不带 circle_id）：
// 查帖子详情拿 circle_id → 审核页定位；查不到（已下架 / 屏蔽 / 删除）→ 提示后去可管理圈子
const resolvingId = ref('')
const openReportedNotice = async (item) => {
  if (resolvingId.value) return
  resolvingId.value = item.id
  try {
    let circleId = ''
    try {
      const res = await getPostDetail(item.post_id)
      circleId = res.data?.circle_id || ''
    } catch {
      // 非 1 状态的帖子对非作者一律 404，与不存在不作区分
    }
    if (circleId) {
      await router.push(reportedQueueTarget(circleId, item.post_id))
      return
    }
    message.warning(t('notice.reportTargetGone'))
    await router.push(await managedCirclesTarget())
  } catch (error) {
    console.error('举报通知跳转失败:', error)
  } finally {
    resolvingId.value = ''
  }
}

// 「我管理的圈子」：只管一个圈子时直接进它的被举报列表；多个（或查询失败）进「我管理的圈子」页自选
const managedCirclesTarget = async () => {
  try {
    const res = await getManagedCircles({ page: 1, size: 2 })
    const circles = res.data ?? []
    if (circles.length === 1) return reportedQueueTarget(circles[0].id)
  } catch (error) {
    console.error('获取可管理圈子失败:', error)
  }
  return MANAGED_CIRCLES_TARGET
}

// 全部已读：本地清零角标 + 列表样式
const handleReadAll = async () => {
  if (readingAll.value) return
  readingAll.value = true
  try {
    await markAllNoticesRead()
    notices.value.forEach(n => { n.is_read = true })
    window.dispatchEvent(new CustomEvent('notice-read-all'))
    message.success(t('notice.readAllSuccess'))
  } catch (error) {
    console.error('全部已读失败:', error)
    message.error(t('notice.readAllFailed'))
  } finally {
    readingAll.value = false
  }
}

// 无限滚动
const loadMoreTrigger = ref(null)
let observer = null

const setupObserver = () => {
  if (!loadMoreTrigger.value) return
  cleanupObserver()
  observer = new IntersectionObserver(
    (entries) => {
      entries.forEach((entry) => {
        if (entry.isIntersecting && hasMore.value && !loading.value) {
          loadNotices()
        }
      })
    },
    { rootMargin: '100px', threshold: 0.1 }
  )
  observer.observe(loadMoreTrigger.value)
}

const cleanupObserver = () => {
  if (observer) {
    observer.disconnect()
    observer = null
  }
}

onMounted(() => {
  loadNotices(true)
})

onUnmounted(() => {
  cleanupObserver()
})
</script>

<style scoped>
.notifications-page {
  min-height: 100vh;
}

.content-wrapper {
  padding-top: var(--header-height);
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.main-content {
  display: flex;
  justify-content: center;
  padding: 24px;
}

.notice-container {
  width: 100%;
  max-width: 720px;
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  box-shadow: var(--shadow-md);
  padding: 20px 24px;
}

.notice-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 8px;
}

.page-title {
  font-size: 20px;
  font-weight: 600;
  color: var(--text-primary);
}

.read-all-btn {
  color: var(--text-secondary);
  border-radius: 16px;
  transition: all 0.2s ease;
}

@media (hover: hover) {
  .read-all-btn:hover {
    color: #8af0d0;
    background: rgba(102, 234, 194, 0.14);
  }
}

/* 类型过滤：顶栏 Tab，全宽下划线式，滚动吸顶 */
.notice-tabs {
  position: sticky;
  top: var(--header-height);
  z-index: 10;
  display: flex;
  gap: 28px;
  margin: 0 -24px 8px;
  padding: 0 24px;
  border-bottom: 1px solid var(--glass-border);
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  overflow-x: auto;
  scrollbar-width: none;
}

.notice-tabs::-webkit-scrollbar {
  display: none;
}

.notice-tab {
  position: relative;
  flex-shrink: 0;
  padding: 12px 2px;
  border: none;
  background: none;
  color: var(--text-secondary);
  font-size: 14px;
  cursor: pointer;
  transition: color 0.2s ease;
}

@media (hover: hover) {
  .notice-tab:hover {
    color: var(--text-primary);
  }
}

.notice-tab.active {
  color: #8af0d0;
  font-weight: 600;
}

/* 激活指示条 */
.notice-tab.active::after {
  content: '';
  position: absolute;
  left: 50%;
  bottom: -1px;
  transform: translateX(-50%);
  width: 24px;
  height: 3px;
  border-radius: 2px;
  background: var(--theme-color);
}

/* 通知列表容器（内层圆角比容器小一档） */
.notice-list {
  margin-top: 30px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

/* 通知条目：整条为链接（SmartLink 渲染 <a>），重置链接默认外观（内层圆角比容器小一档） */
.notice-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  width: 100%;
  padding: 12px 14px;
  border: none;
  border-radius: 10px;
  background: none;
  font: inherit;
  text-align: left;
  text-decoration: none;
  color: inherit;
  cursor: pointer;
  transition: background 0.2s ease;
}

@media (hover: hover) {
  .notice-item:hover {
    background: rgba(102, 234, 194, 0.08);
  }
}

/* 键盘焦点可见指示：Tab 聚焦时清晰可辨，Enter/Space 触发点击 */
.notice-item:focus-visible {
  outline: 2px solid var(--theme-color);
  outline-offset: -2px;
}

/* 未读高亮 */
.notice-item.unread {
  background: rgba(102, 234, 194, 0.06);
}

@media (hover: hover) {
  .notice-item.unread:hover {
    background: rgba(102, 234, 194, 0.12);
  }
}

.notice-avatar {
  flex-shrink: 0;
}

/* 无跳转目标（如关注者已注销）：不可点 */
.notice-item.inert {
  cursor: default;
}

@media (hover: hover) {
  .notice-item.inert:hover {
    background: none;
  }

  .notice-item.inert.unread:hover {
    background: rgba(102, 234, 194, 0.06);
  }
}

.notice-follow-btn {
  flex-shrink: 0;
  align-self: center;
}

/* 系统通知头像：主题绿图标 */
.system-avatar {
  background: rgba(102, 234, 194, 0.14);
  color: #8af0d0;
}

.notice-body {
  flex: 1;
  min-width: 0;
}

.notice-text {
  font-size: 14px;
  line-height: 1.5;
  color: var(--text-primary);
  word-break: break-word;
}

.notice-item.unread .notice-text {
  font-weight: 500;
}

.notice-time {
  margin-top: 4px;
  font-size: 12px;
  color: var(--text-tertiary);
}

.unread-dot {
  flex-shrink: 0;
  width: 8px;
  height: 8px;
  margin-top: 8px;
  border-radius: 50%;
  background: var(--theme-color);
}

.notice-empty {
  padding: 48px 0;
}

.notice-loading {
  display: flex;
  justify-content: center;
  padding: 16px 0;
}

.load-more-trigger {
  height: 1px;
}

.no-more {
  padding: 16px 0;
  text-align: center;
  font-size: 12px;
  color: var(--text-tertiary);
}

@media (max-width: 768px) {
  .main-content {
    padding: 12px;
  }
}
</style>
