<template>
  <div class="my-groups-tab">
    <div class="tab-header">
      <h2 class="tab-title">{{ t('user.myGroups') }}</h2>
      <NInput
        v-model:value="searchKey"
        :placeholder="t('circle.interestCircle')"
        clearable
        round
        class="tab-search"
        @keyup.enter="handleSearch">
        <template #prefix>
          <NIcon>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8"></circle>
              <path d="m21 21-4.35-4.35"></path>
            </svg>
          </NIcon>
        </template>
      </NInput>
      <NButton round @click="handleSearch">
        <template #icon>
          <NIcon>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8"></circle>
              <path d="m21 21-4.35-4.35"></path>
            </svg>
          </NIcon>
        </template>
        {{ t('common.search') }}
      </NButton>
    </div>

    <!-- 角色筛选（单选）：切换即丢弃游标，从首页重新拉取 -->
    <div class="role-filter" role="tablist">
      <button
        v-for="opt in roleOptions"
        :key="opt.value"
        type="button"
        role="tab"
        class="role-filter__item"
        :class="{ 'role-filter__item--active': role === opt.value }"
        :aria-selected="role === opt.value"
        @click="selectRole(opt.value)">
        {{ opt.label }}
      </button>
    </div>

    <NSpin :show="loading">
      <div class="groups-grid" :class="{ 'groups-grid--loading': loading && !circles.length }">
        <div v-if="loadError && !circles.length" class="empty-state">
          <p class="empty-text">{{ t('circle.joinedList.loadFailed') }}</p>
          <NButton round size="small" class="retry-btn" @click="reload">
            {{ t('circle.joinedList.retry') }}
          </NButton>
        </div>
        <div v-else-if="circles.length === 0 && !loading" class="empty-state">
          <NIcon size="64" :depth="3">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
              <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path>
              <circle cx="9" cy="7" r="4"></circle>
              <path d="M23 21v-2a4 4 0 0 0-3-3.87"></path>
              <path d="M16 3.13a4 4 0 0 1 0 7.75"></path>
            </svg>
          </NIcon>
          <p class="empty-text">{{ emptyText }}</p>
          <!-- 查看他人主页、筛选角色或搜索时不展示「创建圈子」引导 -->
          <p v-if="isMine && !keyword && role === ROLE_ALL" class="empty-hint">{{ t('circle.createCircle') }}</p>
        </div>
        <NCard
          v-for="group in circles"
          :key="group.id"
          class="group-card"
          :bordered="false"
          hoverable>
          <!-- 整卡封面链接（stretched-link）：铺满卡片承担圈子跳转，
               右键/中键可新标签页打开。圈子路由目前只认 id（详情接口按 id 查），slug 仅展示 -->
          <SmartLink class="post-cover-link" :to="`/circle/${group.id}`" :aria-label="group.name" />
          <div class="group-info">
            <NAvatar round :size="avatarSize" :src="group.avatar">
              <div v-if="!group.avatar">{{ (group.name || '').charAt(0) }}</div>
            </NAvatar>
            <!-- 头像右侧文字列，两行：名称 + 徽标 / slug · 成员数 · 管理 · 禁言（min-width:0 让省略号生效）-->
            <div class="group-meta">
              <div class="group-title">
                <h3 class="group-name">{{ group.name }}</h3>
                <NTag
                  v-if="roleBadge(group)"
                  class="group-role"
                  size="tiny"
                  round
                  :bordered="false"
                  :type="roleBadge(group).type">
                  {{ roleBadge(group).text }}
                </NTag>
              </div>
              <!-- slug · 成员数 · 管理 不换行（slug 先省略）；禁言标签放不下时整体换到下一行 -->
              <div class="group-sub">
                <span class="group-sub-main">
                  <span v-if="group.slug" class="group-slug">c/{{ group.slug }}</span>
                  <span v-if="group.slug" class="group-dot" aria-hidden="true">·</span>
                  <span class="group-stat">
                    <NIcon size="13">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path>
                        <circle cx="9" cy="7" r="4"></circle>
                      </svg>
                    </NIcon>
                    {{ formatNumber(group.members) }}
                  </span>
                  <!-- 管理入口：第二行右端，叠在整卡链接上层（放名称行会把三列卡片的名称挤到两三个字）。
                       管理页不支持移动端，移动端不显示 -->
                  <SmartLink
                    v-if="canManage(group)"
                    class="group-manage"
                    :to="{ name: 'circle-moderation', params: { id: group.id } }">
                    {{ t('circle.manage.entry') }}
                  </SmartLink>
                </span>
                <NTag
                  v-if="isMuted(group)"
                  class="group-muted"
                  size="tiny"
                  round
                  :bordered="false"
                  type="error">
                  {{ muteText(group) }}
                </NTag>
              </div>
            </div>
          </div>
        </NCard>
        <!-- 无限滚动哨兵 -->
        <div ref="sentinel" class="scroll-sentinel"></div>
        <!-- 翻页失败：保留已加载的列表，就地重试 -->
        <div v-if="loadError && circles.length" class="load-more-error">
          {{ t('circle.joinedList.loadFailed') }}
          <NButton text type="primary" size="small" class="retry-btn" @click="fetchCircles(true)">
            {{ t('circle.joinedList.retry') }}
          </NButton>
        </div>
        <!-- 搜索模式被截断提示（扫满上限仍未集齐 size 条，继续翻页仍能取到结果）-->
        <div v-if="truncated" class="truncated-hint">
          {{ t('circle.resultsMayBeIncomplete') }}
        </div>
      </div>
    </NSpin>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { NCard, NAvatar, NIcon, NInput, NButton, NSpin, NTag } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useFormatNumber } from '@/utils/i18n'
import { getMyCircles, getUserCircles } from '@/api/post'
import SmartLink from '@/components/common/SmartLink.vue'
import { useBreakpoint } from '@/composables/useBreakpoint'
import { useCircleMeta } from '@/composables/useCircleMeta'
import { CIRCLE_ROLE, MEMBER_STATUS, isManager } from '@/constants/circle'

const { t, locale } = useI18n()
const { formatNumber } = useFormatNumber()
const { isMobile } = useBreakpoint()
const { getRoleInfo } = useCircleMeta()

// 头像尺寸用 prop 切换：NAvatar 以内联 CSS 变量定尺寸，CSS 覆盖需 !important
const avatarSize = computed(() => (isMobile.value ? 40 : 44))

const props = defineProps({
  // 目标用户 ID：
  // - 为空：查看「我加入的圈子」，走 /circle/my
  // - 传值：查看该用户加入的圈子，走 /circle/user?user_id=...（访客可读；不返回禁言信息）
  userId: {
    type: String,
    default: ''
  },
  // 当前 Tab 是否激活：离开时清空关键字与角色筛选，切回即为干净的全量列表
  active: {
    type: Boolean,
    default: false
  }
})

const emit = defineEmits(['total-change'])

const isMine = computed(() => !props.userId)

const PAGE_SIZE = 20
const SEARCH_DEBOUNCE = 300

// 角色筛选：ROLE_ALL 表示不传 role
const ROLE_ALL = 0
const ROLE_KEYS = {
  [ROLE_ALL]: 'all',
  [CIRCLE_ROLE.OWNER]: 'owner',
  [CIRCLE_ROLE.ADMIN]: 'admin',
  [CIRCLE_ROLE.MEMBER]: 'member'
}
const roleOptions = computed(() => [
  { value: ROLE_ALL, label: t('circle.joinedList.roleAll') },
  { value: CIRCLE_ROLE.OWNER, label: t('circle.joinedList.roleOwner') },
  { value: CIRCLE_ROLE.ADMIN, label: t('circle.joinedList.roleAdmin') },
  { value: CIRCLE_ROLE.MEMBER, label: t('circle.joinedList.roleMember') }
])

// searchKey 是输入框内容；keyword / role 是当前列表实际对应的查询条件
const searchKey = ref('')
const keyword = ref('')
const role = ref(ROLE_ALL)

const circles = ref([])
const searchAfter = ref('')
const hasMore = ref(false)
const loading = ref(false)
const loadError = ref(false)
// 搜索模式可能被服务端截断（扫满上限仍未集齐 size 条）
const truncated = ref(false)
const sentinel = ref(null)
let observer = null
// 请求序号：查询条件变化后旧请求作废，晚到的响应直接丢弃，避免旧条件的结果/游标串进新列表
let requestSeq = 0

// 后端 snake_case → 组件 camelCase
// /circle/user 不返回 member_status / mute_end_time；mute_end_time 仅禁言生效中返回
const transformCircle = (c) => ({
  id: c.id,
  name: c.name || t('circle.interestCircle'),
  avatar: c.avatar_url || '',
  slug: c.slug || '',
  members: c.member_count || 0,
  role: c.role || CIRCLE_ROLE.MEMBER,
  memberStatus: c.member_status ?? MEMBER_STATUS.ACTIVE,
  muteEndTime: c.mute_end_time || ''
})

// 游标与当前查询条件不匹配时后端返回 400 Invalid search_after parameter
const isCursorError = (error) => error?.code === 400 && /search_after/i.test(error?.message || '')

// 拉取圈子：append=true 表示用游标追加下一页
const fetchCircles = async (append = false) => {
  if (append && (loading.value || !searchAfter.value)) return
  const seq = ++requestSeq
  const query = { keyword: keyword.value, role: role.value }
  loading.value = true
  loadError.value = false
  try {
    const params = { size: PAGE_SIZE }
    if (query.keyword) params.keyword = query.keyword
    if (query.role !== ROLE_ALL) params.role = query.role
    // 游标原样透传，不解析/不修改（不透明串）
    if (append) params.search_after = searchAfter.value

    const res = props.userId
      ? await getUserCircles(props.userId, params)
      : await getMyCircles(params)
    if (seq !== requestSeq) return

    const data = res.data || {}
    const list = (data.circles || []).map(transformCircle)
    circles.value = append ? [...circles.value, ...list] : list
    // 单页可能不满 size（已封禁 / 删除的圈子被过滤），只以 search_after 判断是否到底
    searchAfter.value = data.search_after || ''
    hasMore.value = !!searchAfter.value
    truncated.value = !!data.truncated

    // 父页面 Tab 上的总数只取「全部」首屏：关键字模式 total 恒为 0，角色筛选的数量会覆盖总数。
    // 角色 Tab 不显示数量：各 Tab 的数量要切过去才知道，只显示部分 Tab 的数字反而让人困惑
    if (!append && !query.keyword && query.role === ROLE_ALL) {
      emit('total-change', data.total || 0)
    }
  } catch (error) {
    if (seq !== requestSeq) return
    // 游标失效：丢弃游标，从首页重新加载
    if (append && isCursorError(error)) {
      reload()
      return
    }
    console.error('获取圈子列表失败:', error)
    // 401 已由 request 拦截器统一跳登录，这里不再展示错误
    if (error?.code !== 401) loadError.value = true
  } finally {
    if (seq === requestSeq) loading.value = false
  }
}

// 丢弃游标与旧列表，按当前查询条件从首页重新拉取
const reload = () => {
  searchAfter.value = ''
  hasMore.value = false
  truncated.value = false
  circles.value = []
  fetchCircles(false)
}

// === 关键字搜索：输入防抖，回车 / 按钮 / 清空立即生效 ===
let searchTimer = null
const cancelPendingSearch = () => {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
}

const applyKeyword = () => {
  cancelPendingSearch()
  const next = searchKey.value.trim()
  // 条件未变时不重复请求；上次失败则允许重试
  if (next === keyword.value && !loadError.value) return
  keyword.value = next
  reload()
}

const handleSearch = () => applyKeyword()

watch(searchKey, (value) => {
  cancelPendingSearch()
  if (!(value || '').trim()) {
    applyKeyword()
    return
  }
  searchTimer = setTimeout(applyKeyword, SEARCH_DEBOUNCE)
})

// === 角色筛选 ===
const selectRole = (value) => {
  if (role.value === value) return
  role.value = value
  // 输入框里尚未生效的关键字一并应用，保证列表与输入框一致
  cancelPendingSearch()
  keyword.value = searchKey.value.trim()
  reload()
}

// 清空关键字与角色筛选并重拉首页（切换目标用户 / 离开 Tab 时使用）。
// 先清 keyword 再清 searchKey：searchKey 的 watch 看到条件未变，不会重复请求
const resetFilters = () => {
  cancelPendingSearch()
  keyword.value = ''
  searchKey.value = ''
  role.value = ROLE_ALL
  reload()
}

const emptyText = computed(() => {
  if (keyword.value) return t('circle.noCircleResults')
  const scope = isMine.value ? 'mine' : 'others'
  return t(`circle.joinedList.empty.${scope}.${ROLE_KEYS[role.value]}`)
})

// === 卡片展示 ===
// 普通成员不显示徽标，避免满屏「成员」
const roleBadge = (group) => (isManager(group.role) ? getRoleInfo(group.role) : null)

// 管理入口：仅「我的圈子」里圈主 / 管理员可见；管理页不支持移动端
const canManage = (group) => isMine.value && !isMobile.value && isManager(group.role)

// 当前时间（每分钟刷新）：停留在页面上时，禁言到期后标签自动消失
const now = ref(Date.now())
let clockTimer = null

const isMuted = (group) => {
  if (!isMine.value || group.memberStatus !== MEMBER_STATUS.MUTED) return false
  if (!group.muteEndTime) return true
  return new Date(group.muteEndTime).getTime() > now.value
}

const formatMuteEnd = (time) => {
  const date = new Date(time)
  const options = { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }
  // 禁言最长 30 天，可能跨年：跨年时补上年份
  if (date.getFullYear() !== new Date(now.value).getFullYear()) options.year = 'numeric'
  return date.toLocaleString(locale.value, options)
}

const muteText = (group) => (group.muteEndTime
  ? t('circle.joinedList.mutedUntil', { time: formatMuteEnd(group.muteEndTime) })
  : t('circle.joinedList.muted'))

// === 无限滚动 ===
// 翻页失败时停止自动加载，改由用户点「重试」，避免哨兵可见时反复请求
const setupObserver = () => {
  if (observer) {
    observer.disconnect()
    observer = null
  }
  if (!sentinel.value || !hasMore.value || loading.value || loadError.value) return

  observer = new IntersectionObserver(
    (entries) => {
      if (entries[0].isIntersecting && hasMore.value && !loading.value && !loadError.value) {
        fetchCircles(true)
      }
    },
    { rootMargin: '200px' }
  )
  observer.observe(sentinel.value)
}

watch([sentinel, hasMore, loading, loadError], setupObserver)

// 切换目标用户（路由参数变化）时清空筛选，重新拉取首页
watch(() => props.userId, (next, prev) => {
  if (next === prev) return
  resetFilters()
})

// 离开本 Tab：存在关键字或角色筛选时清空，切回即为干净的全量列表
watch(() => props.active, (next, prev) => {
  if (prev && !next && (keyword.value || searchKey.value || role.value !== ROLE_ALL)) {
    resetFilters()
  }
})

// 圈子跳转已由卡片封面链接（SmartLink）承担：真实 <a href>，
// 支持 hover URL / 右键新标签页；此处不再监听点击（旧版 emit+push 会双跳转）

onMounted(() => {
  nextTick(() => fetchCircles(false))
  if (isMine.value) {
    clockTimer = setInterval(() => {
      now.value = Date.now()
    }, 60_000)
  }
})

onBeforeUnmount(() => {
  if (observer) observer.disconnect()
  cancelPendingSearch()
  if (clockTimer) clearInterval(clockTimer)
})
</script>

<style scoped>
.my-groups-tab {
  padding: 8px 0;
}

.tab-header {
  display: flex;
  align-items: center;
  margin-bottom: 24px;
  flex-wrap: wrap;
  margin-right: 3dvw;
  gap: 16px;
}

.tab-title {
  font-size: 1.5rem;
  margin-right: 3dvw;
  font-weight: 600;
  color: rgba(255, 255, 255, 0.95);
}

/* 角色筛选：单选胶囊组，选中态用主题绿 */
.role-filter {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 20px;
}

.role-filter__item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border: 1px solid var(--glass-border);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.03);
  color: var(--text-secondary);
  font-size: 0.85rem;
  line-height: 1.2;
  cursor: pointer;
  transition: background 0.2s cubic-bezier(0.4, 0, 0.2, 1), color 0.2s cubic-bezier(0.4, 0, 0.2, 1),
    border-color 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

@media (hover: hover) {
  .role-filter__item:hover {
    background: rgba(255, 255, 255, 0.06);
    color: var(--text-primary);
  }
}

.role-filter__item--active,
.role-filter__item--active:hover {
  background: rgba(102, 234, 194, 0.14);
  border-color: rgba(102, 234, 194, 0.35);
  color: #8af0d0;
}

/* 每行 3 个圈子卡片；align-items:start 让卡片按自身内容定高，不被同行最高的卡片拉伸 */
.groups-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  align-items: start;
  gap: 12px;
}

/* 加载占位：列表为空时撑高容器，避免 NSpin 动画被裁剪、页面收缩跳动 */
.groups-grid--loading {
  min-height: 320px;
}

.group-card {
  position: relative;
  background: rgba(255, 255, 255, 0.02) !important;
  border: 1px solid rgba(255, 255, 255, 0.05) !important;
  border-radius: 14px !important;
  transition: all 0.3s ease;
  overflow: hidden;
  cursor: pointer;
}

@media (hover: hover) {
  .group-card:hover {
    background: rgba(255, 255, 255, 0.04) !important;
    border-color: rgba(255, 255, 255, 0.08) !important;
    transform: translateY(-2px);
  }
}

/* 整卡封面链接（stretched-link）：铺满卡片让浏览器在任意位置识别出圈子链接 */
.post-cover-link {
  position: absolute;
  inset: 0;
  z-index: 1;
}

.group-info {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 16px;
}

/* 只保留一层内边距：清掉 NCard 内容区默认内边距。
   多一层 .groups-grid 提高优先级：所在页 .tabs-card :deep(.n-card__content)
   的 padding 会穿透到嵌套的圈子卡片 */
.groups-grid .group-card :deep(.n-card__content) {
  padding: 0;
}

/* 无头像时回退首字母样式（参考 CircleList）*/
.group-info :deep(.n-avatar) {
  flex-shrink: 0;
  color: #fff;
  font-weight: 700;
  font-size: 1.2rem;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}

/* 修正首字母居中：display-directive="show" 下兴趣圈 tab 初始 display:none 挂载，
   naive-ui NAvatar 的 fitTextTransform 首次在隐藏态测得 offsetWidth/Height=0 → ratio=NaN →
   内联 transform(scale NaN) 失效；又因 memoedTextHtml 缓存守卫，切回可见态不再重算，
   导致 .n-avatar__text 停在 left/top:50%（左上角贴圆心）→ 首字母偏到右下角。
   此处强制 translate 基于元素真实尺寸居中（CSS 百分比 translate 渲染即正确，绕过 JS 测量），
   !important 用于覆盖 naive-ui 写入的内联 transform。*/
.group-info :deep(.n-avatar__text) {
  transform: translate(-50%, -50%) !important;
}

/* 头像右侧文字列：两行（名称行 / slug · 成员数 · 禁言） */
.group-meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

/* 名称 + 角色徽标同一行：名称可省略，徽标不收缩 */
.group-title {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.group-name {
  font-size: 1rem;
  font-weight: 600;
  line-height: 1.3;
  color: rgba(255, 255, 255, 0.95);
  margin: 0;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-role {
  flex-shrink: 0;
}

/* 管理入口：第二行右端的小胶囊，z-index 叠在整卡链接（z-index:1）之上 */
.group-manage {
  position: relative;
  z-index: 2;
  flex-shrink: 0;
  margin-left: auto;
  padding: 1px 8px;
  border-radius: 8px;
  font-size: 0.75rem;
  line-height: 1.4;
  color: #8af0d0;
  background: rgba(102, 234, 194, 0.1);
  text-decoration: none;
  transition: background 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.group-manage:hover {
  background: rgba(102, 234, 194, 0.2);
}

/* 第二行：[slug · 成员数 · 管理入口] + 禁言标签；放不下时禁言标签整体换行 */
.group-sub {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 2px 6px;
  min-width: 0;
  font-size: 0.8rem;
  line-height: 1.3;
  color: var(--text-tertiary);
}

/* 主信息不换行：撑满所在行让管理入口靠右；宽度不够时 slug 先省略 */
.group-sub-main {
  flex: 1 1 auto;
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.group-slug {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.group-stat {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  white-space: nowrap;
  color: rgba(255, 255, 255, 0.7);
}

.group-muted {
  flex-shrink: 0;
}

.load-more-error {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 8px 0;
  font-size: 0.85rem;
  color: var(--text-tertiary);
}

.scroll-sentinel {
  grid-column: 1 / -1;
  height: 1px;
}

.truncated-hint {
  grid-column: 1 / -1;
  text-align: center;
  padding: 8px 0;
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.4);
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 60px 20px;
  text-align: center;
  grid-column: 1 / -1;
}

.empty-text {
  font-size: 1.1rem;
  color: rgba(255, 255, 255, 0.6);
  margin: 16px 0 4px 0;
}

.empty-hint {
  font-size: 0.9rem;
  color: rgba(255, 255, 255, 0.4);
  margin: 0;
}

/* 响应式 */
@media (max-width: 1024px) {
  .groups-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 768px) {
  .groups-grid {
    grid-template-columns: 1fr;
  }

  .tab-header {
    flex-direction: column;
    align-items: stretch;
  }
}

/* 搜索框：桌面定宽，移动端占满整行（tab-header 在 768 以下纵向排列） */
.tab-search {
  width: 280px;
}

@media (max-width: 768px) {
  .tab-search {
    width: 100%;
  }
}

/* 移动端：标题与 Tab 名重复，隐藏；搜索框与按钮同一行。
   所在页面（我的 / 用户主页）在移动端左右通栏，这里自留 12px */
@media (max-width: 768px) {
  .tab-header {
    flex-direction: row;
    flex-wrap: nowrap;
    gap: 8px;
    margin-bottom: 12px;
    padding: 0 0;
  }

  .tab-title {
    display: none;
  }

  .tab-search {
    flex: 1;
    min-width: 0;
  }
}

@media (max-width: 768px) {
  .my-groups-tab {
    padding: 0 12px;
  }
}

/* 移动端单列：收紧间距，普通卡片约 64px（#20 / #48）*/
@media (max-width: 768px) {
  .groups-grid {
    gap: 8px;
  }

  .group-card {
    border-radius: 12px !important;
  }

  .group-info {
    padding: 11px 14px;
  }

  .group-meta {
    gap: 3px;
  }

  .group-name {
    font-size: 0.95rem;
  }

  .group-sub {
    font-size: 0.75rem;
  }

  /* 移动端没有管理入口，主信息不必撑满整行：禁言标签紧跟成员数 */
  .group-sub-main {
    flex-grow: 0;
  }

  .group-info :deep(.n-avatar) {
    font-size: 1.1rem;
  }

  .role-filter {
    gap: 6px;
    margin-bottom: 12px;
  }

  .role-filter__item {
    padding: 5px 12px;
  }
}
</style>
