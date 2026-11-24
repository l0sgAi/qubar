<template>
  <div class="managed-circle-list">
    <!-- 加载骨架：与真实卡片同构（头行 + 描述两行 + 统计行），高度≈内容高度，
         tab 切入时外层按此测高做动画不会裁切 -->
    <div v-if="loading && !circles.length" class="circle-grid">
      <div v-for="i in 6" :key="i" class="circle-card circle-card-skeleton">
        <div class="circle-card-head">
          <div class="skel" style="width: 44px; height: 44px; border-radius: 10px" />
          <div class="skel-lines">
            <div class="skel" style="width: 60%; height: 15px" />
            <div class="skel" style="width: 38%; height: 20px" />
          </div>
        </div>
        <div class="skel" style="height: 39px" />
        <div class="skel" style="width: 65%; height: 18px" />
      </div>
    </div>

    <!-- 卡片网格 -->
    <div v-else-if="circles.length" class="circle-grid" :class="{ 'is-refreshing': loading }">
      <NTooltip
        v-for="circle in circles"
        :key="circle.id"
        :disabled="circle.status === 1"
        trigger="hover"
      >
        <template #trigger>
          <div
            class="circle-card"
            :class="{ 'is-disabled': circle.status !== 1 }"
            @click="handleSelect(circle)"
          >
            <div class="circle-card-head">
              <NAvatar
                :src="circle.avatar_url || undefined"
                :size="44"
                round
                class="circle-avatar"
                :style="avatarFallbackStyle"
              >
                <template v-if="circle.avatar_url" #fallback>
                  {{ (circle.name || '?').charAt(0) }}
                </template>
                <template v-else #default>
                  {{ (circle.name || '?').charAt(0) }}
                </template>
              </NAvatar>
              <div class="circle-title">
                <span class="circle-name">{{ circle.name }}</span>
                <NTag
                  :type="circle.my_role === 30 ? 'primary' : 'info'"
                  size="tiny"
                  :bordered="false"
                  round
                >
                  {{ circle.my_role === 30 ? t('agent.circleList.roleOwner') : t('agent.circleList.roleAdmin') }}
                </NTag>
              </div>
            </div>
            <p class="circle-desc">{{ circle.description || t('agent.circleList.noDescription') }}</p>
            <div class="circle-stats">
              <span>{{ t('agent.circleList.members', { count: circle.member_count }) }}</span>
              <span class="dot">·</span>
              <span>{{ t('agent.circleList.posts', { count: circle.post_count }) }}</span>
              <template v-if="target === 'agents'">
                <span class="dot">·</span>
                <span class="agent-quota">{{ t('agent.circleList.agentsBound', { count: circle.agent_count ?? 0 }) }}</span>
              </template>
            </div>
          </div>
        </template>
        {{ t(`${STATUS_TIP_PREFIX[target]}${circle.status === 0 ? 'statusPending' : 'statusBanned'}`) }}
      </NTooltip>
    </div>

    <!-- 空态 -->
    <div v-else class="circle-empty">
      <NIcon size="48" color="rgba(255,255,255,0.25)"><AlbumsIcon /></NIcon>
      <p>{{ keyword.trim() ? t('agent.circleList.emptySearch') : t('agent.circleList.empty') }}</p>
    </div>

    <!-- 分页 -->
    <div v-if="total > pageSize" class="pagination-row">
      <NPagination
        v-model:page="page"
        :page-size="pageSize"
        :item-count="total"
        @update:page="fetchCircles"
      />
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { NIcon, NAvatar, NTag, NTooltip, NPagination, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { LayoutGrid as AlbumsIcon } from '@vicons/tabler'
import { getManagedCircles } from '@/api/circle'
import { useDebounceFn } from '@/utils/throttle'

// 选中圈子后进入的管理页：agents → 机器人管理（默认），moderation → 内容审核（举报通知兜底跳转）
const props = defineProps({
  target: {
    type: String,
    default: 'agents',
    validator: v => ['agents', 'moderation'].includes(v)
  }
})
const TARGET_ROUTES = { agents: 'circle-agents', moderation: 'circle-moderation' }
const STATUS_TIP_PREFIX = { agents: 'agent.circleList.', moderation: 'circle.managed.' }

const { t } = useI18n()
const message = useMessage()
const router = useRouter()

// ---- 列表（offset 分页 + keyword 子串过滤；排序后端固定：圈主在前、同角色建圈新→旧）----
const circles = ref([])
const loading = ref(false)
const page = ref(1)
const pageSize = 20
const total = ref(0)
// 搜索关键词由父级页头输入框持有（与全局机器人搜索同位置），经 v-model:keyword 传入；
// 组件内 watch + 防抖，300ms 后回第一页重查
const keyword = defineModel('keyword', { type: String, default: '' })

// 拉取世代：防抖后仍可能乱序返回（慢请求晚到），过期响应直接丢弃
let fetchGen = 0

const fetchCircles = async () => {
  const gen = ++fetchGen
  loading.value = true
  try {
    const kw = keyword.value.trim()
    const res = await getManagedCircles({ page: page.value, size: pageSize, ...(kw && { keyword: kw }) })
    if (gen !== fetchGen) return
    // 空结果时后端整个省略 data 键（omitempty），必须 ?? [] 兜底
    circles.value = res.data ?? []
    total.value = res.total || 0
    // 请求页码越界不报错（返回空 data + 正确 total）：回跳第一页重拉
    if (page.value > 1 && circles.value.length === 0 && total.value > 0) {
      page.value = 1
      fetchCircles()
    }
  } catch (e) {
    if (gen !== fetchGen) return
    console.error('获取可管理圈子列表失败:', e)
    // 不静默清空列表：保留旧数据，仅提示
    message.error(e.message || t('common.operationFailed'))
  } finally {
    if (gen === fetchGen) loading.value = false
  }
}

// 输入防抖 300ms 后回第一页重查（clearable 清空同样触发）
const debouncedSearch = useDebounceFn(() => {
  page.value = 1
  fetchCircles()
}, 300)
watch(keyword, debouncedSearch)

onMounted(fetchCircles)

// 无头像圈子的占位样式：首字母 + 主题色底（同 SideNav 圈子图标配色）
const avatarFallbackStyle = {
  background: 'rgba(102, 234, 194, 0.18)',
  color: '#8af0d0',
  fontWeight: 600,
  flexShrink: 0
}

// status!==1（审核中/封禁）置灰禁点：存量代理管理入口暂停，但保留展示
const handleSelect = circle => {
  if (circle.status !== 1) return
  router.push({ name: TARGET_ROUTES[props.target], params: { id: circle.id } })
}
</script>

<style scoped>
.managed-circle-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.circle-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 16px;
  transition: opacity 0.2s ease;
}

/* 翻页/搜索刷新中：保留旧卡片降低跳动，整体微透明提示加载中 */
.circle-grid.is-refreshing {
  opacity: 0.55;
  pointer-events: none;
}

.circle-card {
  background: var(--glass-bg);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  padding: 16px;
  cursor: pointer;
  transition: transform 0.2s ease, border-color 0.2s ease, box-shadow 0.2s ease;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.circle-card:hover {
  transform: translateY(-2px);
  border-color: rgba(102, 234, 194, 0.35);
  box-shadow: var(--shadow-md);
}

/* 非正常状态圈子（审核中/封禁）：置灰禁点 */
.circle-card.is-disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.circle-card.is-disabled:hover {
  transform: none;
  border-color: var(--glass-border);
  box-shadow: none;
}

.circle-card-head {
  display: flex;
  align-items: center;
  gap: 12px;
}

/* 修正无头像首字符居中：AdminAgents 的 NTabPane display-directive="show" 下，
   本组件在 display:none 的 pane 内挂载，naive-ui NAvatar 的 fitTextTransform
   首次在隐藏态测得 offsetWidth/Height=0 → ratio=NaN → 内联 transform 失效；
   又因 memoedTextHtml 缓存守卫，切回可见态不再重算，.n-avatar__text 停在
   left/top:50%（左上角贴圆心）→ 首字符偏右下角。CSS 百分比 translate 渲染即正确，
   !important 用于覆盖 naive-ui 写入的内联 transform（内容恒为单字符，可安全
   丢弃其 scale 缩放部分）。同 MyGroups.vue 的既有修复。*/
.circle-avatar :deep(.n-avatar__text) {
  transform: translate(-50%, -50%) !important;
}

/* #fallback 插槽（有 avatar_url 但加载失败）的内容不走 .n-avatar__text 包裹，
   是 .n-avatar（inline-flex）的直接子节点且无对齐规则 → 贴左上角，补 flex 居中；
   不影响 default/img 路径（前者绝对定位有独立偏移，后者铺满 100%）*/
.circle-avatar {
  align-items: center;
  justify-content: center;
}

.circle-title {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  align-items: flex-start;
}

.circle-name {
  color: var(--text-primary);
  font-weight: 600;
  font-size: 15px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.circle-desc {
  margin: 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.5;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  min-height: 39px;
}

.circle-stats {
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--text-tertiary);
  font-size: 12px;
}

.circle-stats .dot {
  opacity: 0.5;
}

.agent-quota {
  color: var(--theme-color);
}

.circle-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 64px 0;
  color: var(--text-tertiary);
}

.pagination-row {
  display: flex;
  justify-content: center;
}

/* 骨架卡片：布局同真实卡片（column 由 .circle-card 继承），仅去指针 */
.circle-card-skeleton {
  cursor: default;
}

.skel-lines {
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
}

.skel {
  border-radius: 4px;
  background: linear-gradient(90deg, rgba(255, 255, 255, 0.08) 25%, rgba(255, 255, 255, 0.2) 37%, rgba(255, 255, 255, 0.08) 63%);
  background-size: 400% 100%;
  animation: circle-skel-shimmer 1.4s ease infinite;
}

@keyframes circle-skel-shimmer {
  0% { background-position: 100% 50%; }
  100% { background-position: 0 50%; }
}
</style>
