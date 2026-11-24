<template>
  <div class="circle-agents-page">
    <!-- 顶栏 -->
    <AppHeader />

    <!-- 侧边栏 -->
    <SideNav @collapsed="offset = 64" @expanded="offset = 260" />

    <!-- 主内容区域 -->
    <div class="content-wrapper" :style="{ 'margin-left': `${offset}px`, width: `calc(100% - ${offset}px)` }">
      <div class="main-content">
        <div class="circle-agents-container">
          <!-- 页头 -->
          <div class="page-header">
            <h1 class="page-title">
              {{ circleName ? t('agent.circlePage.titleWithName', { name: circleName }) : t('title.circleAgents') }}
            </h1>
            <NButton size="small" quaternary class="back-btn" @click="goBack">
              {{ t('agent.circlePage.backToList') }}
            </NButton>
          </div>

          <!-- 圈子信息加载中：轻量占位 -->
          <div v-if="loadingCircle" class="page-loading">
            <NSpin size="large" />
          </div>

          <!-- 非管理角色：无权管理（服务端对本组接口一律 403，直接不发起请求） -->
          <div v-else-if="!isManagerRole" class="no-permission">
            <NIcon size="48" color="rgba(255,255,255,0.25)"><RobotIcon /></NIcon>
            <p>{{ t('agent.circlePage.noPermission') }}</p>
          </div>

          <template v-else>
            <!-- 工具栏：搜索 + 配额 + 新建 -->
            <div class="toolbar">
              <NInput
                v-model:value="keyword"
                size="small"
                round
                clearable
                :placeholder="t('agent.searchPlaceholder')"
                class="search-input"
              >
                <template #prefix>
                  <NIcon size="14"><SearchIcon /></NIcon>
                </template>
              </NInput>
              <div class="toolbar-right">
                <NTag
                  size="small"
                  round
                  :bordered="false"
                  :type="quotaFull ? 'warning' : 'success'"
                  class="quota-tag"
                >
                  {{ t('agent.circlePage.quota', { count: quotaCount }) }}
                </NTag>
                <!-- 每圈上限 5 个：满了禁用新建并给出Tooltip说明（删除后名称与配额即释放） -->
                <NTooltip :disabled="!quotaFull" trigger="hover">
                  <template #trigger>
                    <NButton
                      type="primary"
                      size="small"
                      class="create-btn"
                      :disabled="quotaFull"
                      @click="openCreate"
                    >
                      {{ t('agent.circlePage.create') }}
                    </NButton>
                  </template>
                  {{ t('agent.circlePage.quotaFull') }}
                </NTooltip>
              </div>
            </div>

            <!-- 机器人表格（列结构与 AdminAgents 全局控制台一致） -->
            <NDataTable
              v-if="agents.length || loading"
              :columns="columns"
              :data="agents"
              :loading="loading"
              :row-key="row => row.id"
              :bordered="false"
              :theme-overrides="tableThemeOverrides"
              size="small"
              class="agents-table"
            />

            <!-- 空态（区分搜索无结果 / 本圈无机器人） -->
            <div v-else class="agents-empty">
              <NIcon size="48" color="rgba(255,255,255,0.25)"><RobotIcon /></NIcon>
              <p>{{ keyword.trim() ? t('agent.circlePage.emptySearch') : t('agent.circlePage.empty') }}</p>
            </div>

            <!-- 分页 -->
            <div v-if="total > pageSize" class="pagination-row">
              <NPagination
                v-model:page="page"
                :page-size="pageSize"
                :item-count="total"
                @update:page="fetchAgents"
              />
            </div>
          </template>
        </div>
      </div>
    </div>

    <!-- 创建/编辑弹窗：圈子作用域；凭据字段仅圈主可改 -->
    <AgentReplyModal v-if="replyAgent && isOwnerRole" :agent="replyAgent" circle @close="replyAgent = null" />
    <AgentFormModal
      :key="circleId"
      v-model:show="showFormModal"
      scope="circle"
      :circle-id="circleId"
      :agent="editingAgent"
      :can-edit-credentials="isOwnerRole"
      @success="handleFormSuccess"
    />
  </div>
</template>

<script setup>
import { ref, computed, h, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NButton, NInput, NIcon, NAvatar, NTag, NSwitch, NDataTable, NPagination,
  NPopconfirm, NText, NTooltip, NAlert, NSpin, useMessage
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { RobotOutlined as RobotIcon } from '@vicons/antd'
import { Search as SearchIcon } from '@vicons/tabler'
import AppHeader from '@/components/layout/AppHeader.vue'
import SideNav from '@/components/layout/SideNav.vue'
import AgentReplyModal from '@/components/agent/AgentReplyModal.vue'
import AgentFormModal from '@/components/agent/AgentFormModal.vue'
import { getCircleDetail, getCircleAgentList, updateCircleAgent, deleteCircleAgent } from '@/api/circle'
import { isManager, isOwner } from '@/constants/circle'
import { usePageTitle } from '@/composables/usePageTitle'
import { useDebounceFn } from '@/utils/throttle'

const { t } = useI18n()
const message = useMessage()
const route = useRoute()
const router = useRouter()
const { setTitleData } = usePageTitle()
const offset = ref(260)

const circleId = computed(() => route.params.id)

// ---- 圈子信息：名称用于页头展示；member_role 用于前端权限兜底 ----
// （列表/凭据/删除的最终校验以服务端为准，403 时 toast 透出后端 message）
const circleName = ref('')
const myRole = ref(0)
const loadingCircle = ref(true)

const isManagerRole = computed(() => isManager(myRole.value))
const isOwnerRole = computed(() => isOwner(myRole.value))

// ---- 列表（服务端分页 + keyword 名称子串过滤） ----
const agents = ref([])
const loading = ref(false)
const page = ref(1)
const pageSize = 20
const total = ref(0)
const keyword = ref('')

// 配额计数：仅在无关键词（全量）拉取时更新 total，避免搜索过滤后的
// total < 真实数量误放开「新建」按钮（上限 5）
const quotaCount = ref(0)
const quotaFull = computed(() => quotaCount.value >= 5)

// 拉取世代：防抖后仍可能乱序返回（慢请求晚到），过期响应直接丢弃
let fetchGen = 0

const fetchAgents = async () => {
  if (!isManagerRole.value) return
  const gen = ++fetchGen
  loading.value = true
  try {
    const kw = keyword.value.trim()
    const res = await getCircleAgentList({
      circle_id: circleId.value,
      page: page.value,
      size: pageSize,
      ...(kw && { keyword: kw })
    })
    if (gen !== fetchGen) return
    // 空结果时后端整个省略 data 键（omitempty），必须 ?? [] 兜底
    agents.value = res.data ?? []
    total.value = res.total || 0
    if (!kw) quotaCount.value = total.value
    // 请求页码越界不报错（返回空 data + 正确 total）：回跳第一页重拉
    if (page.value > 1 && agents.value.length === 0 && total.value > 0) {
      page.value = 1
      fetchAgents()
    }
  } catch (e) {
    if (gen !== fetchGen) return
    console.error('获取圈内机器人列表失败:', e)
    // 不静默清空列表：保留旧数据，仅提示（权限被撤等场景由 message 透出）
    message.error(e.message || t('agent.circlePage.loadFailed'))
  } finally {
    if (gen === fetchGen) loading.value = false
  }
}

// 输入防抖 300ms 后回第一页重查（clearable 清空同样触发）
const debouncedSearch = useDebounceFn(() => {
  page.value = 1
  fetchAgents()
}, 300)
watch(keyword, debouncedSearch)

// ---- 触发模式文案 ----
const triggerModeText = mode =>
  ({ 1: t('agent.triggerModes.all'), 2: t('agent.triggerModes.keyword'), 3: t('agent.triggerModes.manual') })[mode] || t('common.unknown')

const formatTime = ts => {
  if (!ts) return ''
  try {
    // RFC3339 带时区，直接解析
    return new Date(ts).toLocaleString()
  } catch {
    return ts
  }
}

// ---- 状态开关：PUT { status } 部分更新（运营字段，admin+ 均可） ----
const toggleStatus = async (row, value) => {
  try {
    const res = await updateCircleAgent(row.id, { status: value ? 1 : 0 })
    const idx = agents.value.findIndex(a => a.id === row.id)
    if (idx !== -1) agents.value[idx] = res.data || { ...agents.value[idx], status: value ? 1 : 0 }
  } catch (e) {
    message.error(e.message || t('agent.statusUpdateFailed'))
    fetchAgents()
  }
}

// ---- 删除：仅圈主；软删不可恢复，二次确认 ----
const handleDelete = async row => {
  try {
    await deleteCircleAgent(row.id)
    message.success(t('agent.deleteSuccess'))
    refreshAfterMutation()
  } catch (e) {
    message.error(e.message || t('common.operationFailed'))
  }
}

// 创建/删除后的刷新：回第一页做一次全量拉取（新机器人置顶、配额计数刷新；
// 新机器人不一定命中旧关键词，且全量 total 才是配额数据源）。清空关键词时
// 会触发防抖重查，无须再显式拉一次，保证每次变更只发一个请求
const refreshAfterMutation = () => {
  page.value = 1
  if (keyword.value) {
    keyword.value = ''
  } else {
    fetchAgents()
  }
}

// ---- 表格外观：官方 DataTable 主题变量（与 AdminAgents 同配方） ----
const tableThemeOverrides = {
  DataTable: {
    borderRadius: '16px',
    // td 透明让外层玻璃底透出，消除内部方角深色底
    tdColor: 'transparent',
    tdColorHover: 'rgba(102, 234, 194, 0.05)',
    // 表头微亮底 + 三级文字色（值同 --text-tertiary）
    thColor: 'rgba(255, 255, 255, 0.04)',
    thTextColor: '#8b8b9e',
    thFontWeight: '600',
    // 行分隔线换成玻璃描边同色系
    borderColor: 'rgba(255, 255, 255, 0.05)'
  }
}

// ---- 表格列 ----
const columns = computed(() => [
  {
    title: t('agent.table.name'),
    key: 'name',
    render: row => h('div', { class: 'c-agent-cell-name' }, [
      // 注意：NAvatar 的 default 插槽优先于 src（插槽有内容就永远渲染文本），
      // 无头像时才给字母插槽；有头像时改用 fallback 插槽兜底加载失败
      h(NAvatar, {
        src: row.avatar_url || undefined,
        size: 28,
        round: true,
        style: { flexShrink: 0, background: 'rgba(102, 234, 194, 0.18)', color: '#8af0d0', fontWeight: 600 }
      }, row.avatar_url
        ? { fallback: () => (row.name || '?').charAt(0) }
        : { default: () => (row.name || '?').charAt(0) }),
      h('div', { class: 'c-agent-cell-name-text' }, [
        h('span', { class: 'c-agent-cell-name-label' }, row.name),
        h(NText, { depth: 3, style: 'font-size: 12px' }, { default: () => row.model })
      ])
    ])
  },
  {
    title: t('agent.table.protocol'),
    key: 'api_protocol',
    width: 110,
    render: row => h(NTag, { size: 'small', bordered: false }, { default: () => row.api_protocol })
  },
  {
    title: t('agent.table.trigger'),
    key: 'trigger',
    width: 200,
    render: row => h('div', { class: 'c-agent-cell-trigger' }, [
      h('span', { class: 'c-agent-cell-trigger-mode' }, triggerModeText(row.trigger_mode)),
      ...(row.trigger_mode === 2 && row.trigger_keywords?.length
        ? [h('div', { class: 'c-agent-cell-keywords' }, [
            ...row.trigger_keywords.slice(0, 3).map(kw =>
              h(NTag, { size: 'tiny', bordered: false, type: 'info', key: kw }, { default: () => kw })
            ),
            row.trigger_keywords.length > 3
              ? h(NText, { depth: 3, style: 'font-size: 11px' }, { default: () => `+${row.trigger_keywords.length - 3}` })
              : null
          ].filter(Boolean))]
        : [])
    ])
  },
  {
    title: t('agent.table.rateLimit'),
    key: 'rate',
    width: 150,
    render: row => h('span', { class: 'c-agent-cell-rate' }, [
      `${row.max_replies_per_hour === 0 ? t('agent.unlimited') : row.max_replies_per_hour}${t('agent.form.perHour')}`,
      ' · ',
      `${row.min_interval_sec === 0 ? t('agent.unlimited') : row.min_interval_sec}${t('agent.form.seconds')}`
    ])
  },
  {
    title: t('agent.table.status'),
    key: 'status',
    width: 90,
    render: row => h(NSwitch, {
      value: row.status === 1,
      size: 'small',
      'onUpdate:value': v => toggleStatus(row, v)
    })
  },
  {
    title: t('agent.table.createTime'),
    key: 'create_time',
    width: 170,
    render: row => h(NText, { depth: 3, style: 'font-size: 12px' }, { default: () => formatTime(row.create_time) })
  },
  {
    title: t('agent.table.actions'),
    key: 'actions',
    width: isOwnerRole.value ? 200 : 90,
    render: row => h('div', { class: 'c-agent-cell-actions' }, [
      isOwnerRole.value && row.status === 1 && row.trigger_mode === 3
        ? h(NButton, { size: 'tiny', onClick: () => { replyAgent.value = row } }, { default: () => t('agent.manualReply.action') })
        : null,
      h(NButton, { size: 'tiny', quaternary: true, onClick: () => openEdit(row) }, { default: () => t('common.edit') }),
      // 删除仅圈主：管理员渲染即 403，前端直接不展示入口
      ...(isOwnerRole.value
        ? [h(NPopconfirm, {
            onPositiveClick: () => handleDelete(row)
          }, {
            trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => t('common.delete') }),
            default: () => t('agent.deleteConfirm', { name: row.name })
          })]
        : [])
    ])
  }
])

// ---- 创建/编辑弹窗 ----
const showFormModal = ref(false)
const editingAgent = ref(null)
const replyAgent = ref(null)

const openCreate = () => {
  editingAgent.value = null
  showFormModal.value = true
}

const openEdit = row => {
  editingAgent.value = row
  showFormModal.value = true
}

const handleFormSuccess = vo => {
  if (editingAgent.value) {
    // 编辑成功：用返回的完整对象局部替换
    const idx = agents.value.findIndex(a => a.id === vo.id)
    if (idx !== -1) agents.value[idx] = vo
  } else {
    // 创建成功：回第一页全量展示
    refreshAfterMutation()
  }
}

// 同一路由切换圈子会复用页面，权限和操作目标都要随圈子重新取。
watch(circleId, async (id, _, onCleanup) => {
  let current = true
  onCleanup(() => { current = false })
  fetchGen++
  loadingCircle.value = true
  myRole.value = 0
  circleName.value = ''
  replyAgent.value = null
  editingAgent.value = null
  showFormModal.value = false
  agents.value = []
  quotaCount.value = 0
  total.value = 0
  page.value = 1
  if (!id) return
  try {
    const res = await getCircleDetail(id)
    if (!current) return
    circleName.value = res.data?.name || ''
    myRole.value = res.data?.member_role || 0
    if (circleName.value) setTitleData('title.circleAgentsName', { name: circleName.value })
    if (isManagerRole.value) fetchAgents()
  } catch (error) {
    if (current) message.error(error.message || t('agent.circlePage.loadFailed'))
  } finally {
    if (current) loadingCircle.value = false
  }
}, { immediate: true })

const goBack = () => router.push('/admin/agents')
</script>

<style scoped>
.circle-agents-page {
  min-height: 100vh;
}

.content-wrapper {
  display: flex;
  padding-top: var(--header-height);
}

.main-content {
  flex: 1;
  min-width: 0;
  padding: 24px;
}

/* 主容器：暗色玻璃拟态大卡片（与 AdminAgents 同一配方） */
.circle-agents-container {
  max-width: 1100px;
  margin: 0 auto;
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid var(--glass-border);
  border-radius: 24px;
  box-shadow: var(--shadow-lg);
  padding: 28px;
  animation: circle-agents-enter 0.25s cubic-bezier(0.34, 1.56, 0.64, 1) both;
}

@keyframes circle-agents-enter {
  from {
    opacity: 0;
    transform: scale(0.98) translateY(8px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

.page-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
  flex-wrap: wrap;
}

/* 标题：主题绿渐变文字 */
.page-title {
  font-size: 1.4rem;
  font-weight: 700;
  margin: 0;
  background: var(--primary-gradient);
  -webkit-background-clip: text;
  background-clip: text;
  color: transparent;
}

.back-btn {
  border-radius: 10px;
}

/* 圈子信息加载中的轻量占位 */
.page-loading {
  display: flex;
  justify-content: center;
  padding: 72px 0;
}

/* 非管理角色：无权面板（内层玻璃卡片） */
.no-permission {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 72px 24px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  color: var(--text-secondary);
}

/* 工具栏：左搜索右动作 */
.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 16px;
  margin-bottom: 14px;
  flex-wrap: wrap;
}

.search-input {
  width: 220px;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 12px;
}

.quota-tag {
  font-variant-numeric: tabular-nums;
}

/* 新建按钮：主题渐变实底 + 悬浮微抬升 */
.create-btn {
  border-radius: 12px;
  background: var(--primary-gradient) !important;
  border: none !important;
  color: #06281c !important;
  font-weight: 600;
  transition: transform 0.2s ease, box-shadow 0.2s ease;
}

.create-btn:not(:disabled):hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 20px rgba(102, 234, 194, 0.35);
}

.create-btn:not(:disabled):active {
  transform: translateY(0);
}

/* 本期不回复提示条：绿色玻璃风（NAlert 非 teleport 组件，scoped :deep 可命中） */
.reply-note {
  margin-bottom: 14px;
  border-radius: 12px !important;
  background: rgba(102, 234, 194, 0.06) !important;
  border: 1px solid rgba(102, 234, 194, 0.18) !important;
}

.reply-note :deep(.n-alert-body__title) {
  color: #8af0d0 !important;
}

/* 表格外壳：仅装饰（微透底 + 玻璃描边）；圆角/行悬浮/表头全部由
   theme-overrides 的 DataTable 变量出，单一样式来源，无双层圆角 */
.agents-table {
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  overflow: hidden;
}

/* 空态 */
.agents-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 64px 0;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  color: var(--text-tertiary);
}

.pagination-row {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

/* 分页：圆角页码，激活项主题绿 */
.pagination-row :deep(.n-pagination-item) {
  border-radius: 10px;
}

.pagination-row :deep(.n-pagination-item--active) {
  background: rgba(102, 234, 194, 0.16);
  color: #8af0d0;
}
</style>

<!-- 全局块：columns render 函数渲染的单元格 DOM。
     这些元素由 NDataTable 的渲染实例创建，不带本组件 scoped 属性，
     scoped CSS 永远选不中（不是优先级问题），必须写非 scoped。
     类名统一 c-agent-cell- 前缀（c=circle），避免与 AdminAgents 的
     agent-cell-* 全局类互相耦合。 -->
<style>
.c-agent-cell-name {
  display: flex;
  align-items: center;
  gap: 10px;
}

.c-agent-cell-name-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.c-agent-cell-name-text .c-agent-cell-name-label {
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.c-agent-cell-trigger {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.c-agent-cell-trigger-mode {
  font-size: 12px;
  color: var(--text-secondary);
}

.c-agent-cell-keywords {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.c-agent-cell-rate {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.c-agent-cell-actions {
  display: flex;
  gap: 4px;
}

/* render 函数内的 Naive UI 组件同样脱离 scoped：标签改胶囊、操作按钮改圆角 */
.c-agent-cell-actions .n-button {
  border-radius: 999px;
}

.circle-agents-page .agents-table .n-tag {
  border-radius: 999px;
}
</style>
