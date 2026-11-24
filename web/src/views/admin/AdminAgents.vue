<template>
  <div class="admin-agents-page">
    <!-- 顶栏 -->
    <AppHeader />

    <!-- 侧边栏 -->
    <SideNav @collapsed="offset = 64" @expanded="offset = 260" />

    <!-- 主内容区域 -->
    <div class="content-wrapper" :style="{ 'margin-left': `${offset}px`, width: `calc(100% - ${offset}px)` }">
      <div class="main-content">
        <div class="agents-container">
          <!-- 页头 -->
          <div class="agents-header">
            <div class="header-left">
              <h1 class="page-title">{{ t('agent.title') }}</h1>
            </div>
            <!-- 搜索框固定在页头右侧（两个 tab 同一位置，切换不跳动）；新建按钮仅全局 tab -->
            <div v-if="!checkingRole" class="header-actions">
              <NInput
                v-if="isAdmin && activeTab === 'global'"
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
              <NInput
                v-else
                v-model:value="circleKeyword"
                size="small"
                round
                clearable
                :placeholder="t('agent.circleList.searchPlaceholder')"
                class="search-input"
              >
                <template #prefix>
                  <NIcon size="14"><SearchIcon /></NIcon>
                </template>
              </NInput>
              <NButton
                v-if="isAdmin && activeTab === 'global'"
                type="primary"
                size="small"
                class="create-btn"
                @click="openCreate"
              >
                {{ t('agent.create') }}
              </NButton>
            </div>
          </div>

          <!-- 角色查询中：轻量加载占位 -->
          <div v-if="checkingRole" class="page-loading">
            <NSpin size="large" />
          </div>

          <!-- 管理员：双 tab（全局机器人管理 + 可管理圈子） -->
          <NTabs v-else-if="isAdmin" v-model:value="activeTab" type="line" animated class="agents-tabs">
            <!-- display-directive="show"：pane 挂载后常驻（v-show 切显隐），circles 数据随页面
                 加载预取；切换只翻转显隐，animated 的高度动画量到的是最终内容高度，
                 不会出现「数据返回换内容把未伸展容器撑裁切」的竞态 -->
            <NTabPane name="global" :tab="t('agent.tabs.global')" display-directive="show">
              <!-- 机器人表格：外观走官方 theme-overrides（圆角/透明底/悬浮色），
                   不与组件默认样式打架，避免「外圆内方」双层圆角 -->
              <NDataTable
                :columns="columns"
                :data="agents"
                :loading="loading"
                :row-key="row => row.id"
                :bordered="false"
                :theme-overrides="tableThemeOverrides"
                size="small"
                class="agents-table"
              />

              <!-- 分页 -->
              <div v-if="total > pageSize" class="pagination-row">
                <NPagination
                  v-model:page="page"
                  :page-size="pageSize"
                  :item-count="total"
                  @update:page="fetchAgents"
                />
              </div>
            </NTabPane>
            <NTabPane name="circles" :tab="t('agent.tabs.circles')" display-directive="show">
              <ManagedCircleList v-model:keyword="circleKeyword" />
            </NTabPane>
          </NTabs>

          <!-- 非管理员：仅可管理圈子列表（选择圈子进入其代理管理页） -->
          <ManagedCircleList v-else v-model:keyword="circleKeyword" />
        </div>
      </div>
    </div>

    <!-- 创建/编辑弹窗 -->
    <AgentReplyModal v-if="replyAgent && isAdmin" :agent="replyAgent" @close="replyAgent = null" />
    <AgentFormModal
      v-model:show="showFormModal"
      :agent="editingAgent"
      @success="handleFormSuccess"
    />
  </div>
</template>

<script setup>
import { ref, computed, h, onMounted, watch } from 'vue'
import {
  NDataTable, NButton, NInput, NIcon, NAvatar, NTag, NSwitch,
  NPagination, NPopconfirm, NText, NTabs, NTabPane, NSpin, useMessage
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Search as SearchIcon } from '@vicons/tabler'
import AppHeader from '@/components/layout/AppHeader.vue'
import SideNav from '@/components/layout/SideNav.vue'
import AgentReplyModal from '@/components/agent/AgentReplyModal.vue'
import AgentFormModal from '@/components/agent/AgentFormModal.vue'
import ManagedCircleList from '@/components/circle/ManagedCircleList.vue'
import { getAgentList, updateAgent, deleteAgent } from '@/api/agent'
import { getUserInfo } from '@/api/auth'
import { useDebounceFn } from '@/utils/throttle'

const { t } = useI18n()
const message = useMessage()
const offset = ref(260)

// ---- 权限自查：后端约定管理员为 role=1 ----
// 管理员：双 tab（全局机器人管理 / 可管理圈子）；非管理员：仅圈子列表
const isAdmin = ref(false)
const checkingRole = ref(true)
const activeTab = ref('global')

onMounted(async () => {
  try {
    const res = await getUserInfo()
    isAdmin.value = res.data?.role === 1
  } catch (e) {
    console.error('获取用户信息失败:', e)
    isAdmin.value = false
  } finally {
    checkingRole.value = false
  }
  if (isAdmin.value) fetchAgents()
})

// ---- 列表（服务端分页 + keyword 名称模糊匹配，create_time 倒序） ----
const agents = ref([])
const loading = ref(false)
const page = ref(1)
const pageSize = 20
const total = ref(0)

// 搜索关键词：ILIKE 名称匹配；空 = 全量
const keyword = ref('')
// 圈子列表搜索关键词（页头搜索框与 ManagedCircleList 共享，防抖在组件内）
const circleKeyword = ref('')

// 拉取世代：防抖后仍可能乱序返回（慢请求晚到），过期响应直接丢弃
let fetchGen = 0

const fetchAgents = async () => {
  const gen = ++fetchGen
  loading.value = true
  try {
    const kw = keyword.value.trim()
    const res = await getAgentList({ page: page.value, size: pageSize, ...(kw && { keyword: kw }) })
    if (gen !== fetchGen) return
    agents.value = res.data || []
    total.value = res.total || 0
  } catch (e) {
    if (gen !== fetchGen) return
    console.error('获取机器人列表失败:', e)
    message.error(e.message || t('common.operationFailed'))
    agents.value = []
    total.value = 0
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

// ---- 状态开关：PUT { status } 部分更新 ----
const toggleStatus = async (row, value) => {
  try {
    const res = await updateAgent(row.id, { status: value ? 1 : 0 })
    const idx = agents.value.findIndex(a => a.id === row.id)
    if (idx !== -1) agents.value[idx] = res.data || { ...agents.value[idx], status: value ? 1 : 0 }
  } catch (e) {
    message.error(e.message || t('agent.statusUpdateFailed'))
    fetchAgents()
  }
}

// ---- 删除：软删不可恢复，二次确认 ----
const handleDelete = async row => {
  try {
    await deleteAgent(row.id)
    message.success(t('agent.deleteSuccess'))
    // 当前页删空后回退一页
    if (agents.value.length === 1 && page.value > 1) page.value -= 1
    fetchAgents()
  } catch (e) {
    message.error(e.message || t('common.operationFailed'))
  }
}

// ---- 表格外观：官方 DataTable 主题变量（比 :deep() 硬覆盖稳，值直接进组件 CSS var）----
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
    render: row => h('div', { class: 'agent-cell-name' }, [
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
      h('div', { class: 'agent-cell-name-text' }, [
        h('span', { class: 'agent-cell-name-label' }, row.name),
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
    render: row => h('div', { class: 'agent-cell-trigger' }, [
      h('span', { class: 'agent-cell-trigger-mode' }, triggerModeText(row.trigger_mode)),
      ...(row.trigger_mode === 2 && row.trigger_keywords?.length
        ? [h('div', { class: 'agent-cell-keywords' },
            row.trigger_keywords.slice(0, 3).map(kw =>
              h(NTag, { size: 'tiny', bordered: false, type: 'info', key: kw }, { default: () => kw })
            ),
            row.trigger_keywords.length > 3
              ? h(NText, { depth: 3, style: 'font-size: 11px' }, { default: () => `+${row.trigger_keywords.length - 3}` })
              : null
          )]
        : [])
    ])
  },
  {
    title: t('agent.table.rateLimit'),
    key: 'rate',
    width: 150,
    render: row => h('span', { class: 'agent-cell-rate' }, [
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
    width: 200,
    render: row => h('div', { class: 'agent-cell-actions' }, [
      isAdmin.value && row.status === 1 && row.trigger_mode === 3
        ? h(NButton, { size: 'tiny', onClick: () => { replyAgent.value = row } }, { default: () => t('agent.manualReply.action') })
        : null,
      h(NButton, { size: 'tiny', quaternary: true, onClick: () => openEdit(row) }, { default: () => t('common.edit') }),
      h(NPopconfirm, {
        onPositiveClick: () => handleDelete(row)
      }, {
        trigger: () => h(NButton, { size: 'tiny', quaternary: true, type: 'error' }, { default: () => t('common.delete') }),
        default: () => t('agent.deleteConfirm', { name: row.name })
      })
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
    // 编辑成功：用返回的完整 AgentVO 局部替换
    const idx = agents.value.findIndex(a => a.id === vo.id)
    if (idx !== -1) agents.value[idx] = vo
  } else {
    // 创建成功：回到第一页展示（列表按 create_time 倒序，新机器人置顶）
    page.value = 1
    fetchAgents()
  }
}
</script>

<style scoped>
.admin-agents-page {
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

/* 主容器：暗色玻璃拟态大卡片（外层圆角最大 24px，内层元素递减） */
.agents-container {
  max-width: 1100px;
  margin: 0 auto;
  background: var(--glass-bg);
  backdrop-filter: blur(24px) saturate(160%);
  -webkit-backdrop-filter: blur(24px) saturate(160%);
  border: 1px solid var(--glass-border);
  border-radius: 24px;
  box-shadow: var(--shadow-lg);
  padding: 28px;
  /* 进入动画：回弹缓动（离开由路由卸载，无需配对） */
  animation: agents-page-enter 0.25s cubic-bezier(0.34, 1.56, 0.64, 1) both;
}

@keyframes agents-page-enter {
  from {
    opacity: 0;
    transform: scale(0.98) translateY(8px);
  }
  to {
    opacity: 1;
    transform: none;
  }
}

.agents-header {
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

.header-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.search-input {
  width: 220px;
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

.create-btn:hover {
  transform: translateY(-1px);
  box-shadow: 0 6px 20px rgba(102, 234, 194, 0.35);
}

.create-btn:active {
  transform: translateY(0);
}

/* 角色查询中的轻量加载占位 */
.page-loading {
  display: flex;
  justify-content: center;
  padding: 72px 0;
}

/* Tab：下划线激活项用主题绿 */
.agents-tabs {
  margin-top: -4px;
}

.agents-tabs :deep(.n-tabs-tab) {
  border-radius: 8px;
}

/* 表格外壳：仅装饰（微透底 + 玻璃描边）；圆角/行悬浮/表头全部由
   theme-overrides 的 DataTable 变量出，单一样式来源，无双层圆角 */
.agents-table {
  background: rgba(255, 255, 255, 0.02);
  border: 1px solid var(--glass-border);
  border-radius: 16px;
  overflow: hidden;
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
     类名统一 agent-cell- 前缀避免全局冲突。 -->
<style>
.agent-cell-name {
  display: flex;
  align-items: center;
  gap: 10px;
}

.agent-cell-name-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.agent-cell-name-text .agent-cell-name-label {
  font-weight: 600;
  color: var(--text-primary);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.agent-cell-trigger {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.agent-cell-trigger-mode {
  font-size: 12px;
  color: var(--text-secondary);
}

.agent-cell-keywords {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  align-items: center;
}

.agent-cell-rate {
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.agent-cell-actions {
  display: flex;
  gap: 4px;
}

/* render 函数内的 Naive UI 组件同样脱离 scoped：标签改胶囊、操作按钮改圆角 */
.agent-cell-actions .n-button {
  border-radius: 999px;
}

.agents-table .n-tag {
  border-radius: 999px;
}
</style>
