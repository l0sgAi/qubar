<template>
  <div class="circle-members-page">
    <!-- 顶栏 -->
    <AppHeader />

    <!-- 侧边栏 -->
    <SideNav @collapsed="offset = 64" @expanded="offset = 260" />

    <!-- 主内容区域 -->
    <div class="main-content" :style="{ 'margin-left': `${offset}px`, width: `calc(100% - ${offset}px)` }">
      <!-- 页面级加载骨架 -->
      <div v-if="pageLoading" class="page-skeleton">
        <div class="sk-brief">
          <div class="sk-avatar"></div>
          <div class="sk-lines">
            <div class="sk-line sk-line--title"></div>
            <div class="sk-line sk-line--sub"></div>
          </div>
        </div>
        <div class="sk-tabs"></div>
        <div class="sk-row" v-for="n in 5" :key="n"></div>
      </div>

      <template v-else>
        <!-- 管理页共享页头：返回 + 圈子信息 + 角色 + 成员管理/编辑资料切换 -->
        <CircleAdminHeader active="members" :circle="circle" />

        <!-- 成员 Tab：正常成员(1) / 禁言中(2) / 已拉黑(3)（入圈审核已下线，后端 #168）；搜索框固定在 tab 行右侧 -->
        <div class="members-panel">
          <NTabs v-model:value="activeTab" type="line" animated size="large">
            <!-- 搜索框：作用于当前 Tab（服务端按用户名/邮箱匹配，可与 status 过滤叠加） -->
            <template #suffix>
              <NInput
                v-model:value="searchInput"
                class="member-search"
                clearable
                :placeholder="t('circle.manage.searchPlaceholder')"
              >
                <template #prefix>
                  <NIcon size="14"><SearchIcon /></NIcon>
                </template>
              </NInput>
            </template>
            <NTabPane v-for="tab in tabDefs" :key="tab.key" :name="tab.key" display-directive="show:lazy">
              <template #tab>{{ tab.label }}</template>

              <div class="member-list">
                <!-- 空态：搜索时区分「无匹配」与「暂无成员」 -->
                <NEmpty
                  v-if="!tabState[tab.key].loading && tabState[tab.key].members.length === 0"
                  :description="tabState[tab.key].keyword
                    ? t('circle.manage.noSearchResult')
                    : t('circle.manage.empty')"
                  class="list-empty"
                />

                <!-- 成员行 -->
                <div
                  v-for="m in tabState[tab.key].members"
                  :key="m.user_id"
                  class="member-row"
                >
                  <NAvatar round :size="40" :src="m.avatar_url || undefined">
                    <div v-if="!m.avatar_url">{{ (m.username || '').charAt(0).toUpperCase() || '?' }}</div>
                  </NAvatar>

                  <div class="member-main">
                    <div class="member-name-row">
                      <SmartLink :to="`/user/${m.user_id}`" class="member-name">
                        {{ m.username || t('circle.manage.unknownUser') }}
                      </SmartLink>
                      <NTag
                        v-if="m.role >= 20"
                        size="tiny"
                        round
                        :type="getRoleInfo(m.role).type"
                      >
                        {{ getRoleInfo(m.role).text }}
                      </NTag>
                    </div>
                    <div class="member-meta">
                      <span class="meta-item">{{ t('circle.manage.joinAt', { time: formatTime(m.join_time) }) }}</span>
                      <span v-if="m.status === MEMBER_STATUS.MUTED && muteRemaining(m)" class="meta-item meta-muted">
                        {{ t('circle.manage.muteRemaining', { time: muteRemaining(m) }) }}
                      </span>
                    </div>
                  </div>

                  <!-- 行内操作：按权限矩阵显隐，目标 role >= 自己一律不渲染 -->
                  <div class="row-actions">
                    <!-- 正常成员：禁言 / 移出 / 拉黑（圈主额外有任免与转让菜单） -->
                    <template v-if="tab.key === 'normal'">
                      <template v-if="canModerate(myRole, m.role)">
                        <NButton
                          size="tiny"
                          quaternary
                          round
                          :disabled="actingId === m.user_id"
                          @click="openMuteModal(m)"
                        >
                          {{ t('circle.manage.actions.mute') }}
                        </NButton>
                        <NButton
                          size="tiny"
                          quaternary
                          round
                          type="warning"
                          class="action-remove"
                          :disabled="actingId === m.user_id"
                          @click="handleRemove(m)"
                        >
                          {{ t('circle.manage.actions.remove') }}
                        </NButton>
                        <NButton
                          size="tiny"
                          quaternary
                          round
                          type="error"
                          :disabled="actingId === m.user_id"
                          @click="handleBan(m)"
                        >
                          {{ t('circle.manage.actions.ban') }}
                        </NButton>
                      </template>
                      <AppDropdown
                        v-if="isOwner(myRole) && canTarget(myRole, m.role)"
                        trigger="click"
                        :options="rowMenuOptions(m)"
                        @select="(key) => handleRowMenu(key, m)"
                      >
                        <NButton quaternary circle size="tiny">
                          <template #icon>
                            <NIcon>
                              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <circle cx="12" cy="12" r="1"></circle>
                                <circle cx="19" cy="12" r="1"></circle>
                                <circle cx="5" cy="12" r="1"></circle>
                              </svg>
                            </NIcon>
                          </template>
                        </NButton>
                      </AppDropdown>
                    </template>

                    <!-- 禁言中：解除禁言 / 移出 / 拉黑 -->
                    <template v-else-if="tab.key === 'muted'">
                      <template v-if="canModerate(myRole, m.role)">
                        <NButton
                          size="tiny"
                          quaternary
                          round
                          :disabled="actingId === m.user_id"
                          @click="handleUnmute(m)"
                        >
                          {{ t('circle.manage.actions.unmute') }}
                        </NButton>
                        <NButton
                          size="tiny"
                          quaternary
                          round
                          type="warning"
                          class="action-remove"
                          :disabled="actingId === m.user_id"
                          @click="handleRemove(m)"
                        >
                          {{ t('circle.manage.actions.remove') }}
                        </NButton>
                        <NButton
                          size="tiny"
                          quaternary
                          round
                          type="error"
                          :disabled="actingId === m.user_id"
                          @click="handleBan(m)"
                        >
                          {{ t('circle.manage.actions.ban') }}
                        </NButton>
                      </template>
                    </template>

                    <!-- 已拉黑：解除拉黑 -->
                    <template v-else-if="tab.key === 'banned'">
                      <NButton
                        v-if="canModerate(myRole, m.role)"
                        size="tiny"
                        quaternary
                        round
                        :disabled="actingId === m.user_id"
                        @click="handleUnban(m)"
                      >
                        {{ t('circle.manage.actions.unban') }}
                      </NButton>
                    </template>
                  </div>
                </div>

                <!-- 首次加载骨架 / 追加翻页 spinner -->
                <div v-if="tabState[tab.key].loading && tabState[tab.key].members.length === 0" class="member-skeleton">
                  <div class="sk-row" v-for="n in 5" :key="n"></div>
                </div>
                <div v-else-if="tabState[tab.key].loading" class="loading-state">
                  <NSpin size="small" />
                </div>
                <div
                  v-else-if="!tabState[tab.key].hasMore && tabState[tab.key].members.length > 0"
                  class="list-footer"
                >
                  {{ t('common.noMore') }}
                </div>

                <div :ref="setSentinel(tab.key)" class="load-sentinel"></div>
              </div>
            </NTabPane>
          </NTabs>
        </div>
      </template>
    </div>

    <!-- 禁言时长选择弹窗 -->
    <NModal
      v-model:show="muteModalVisible"
      preset="card"
      :title="t('circle.manage.muteTitle')"
      :style="{ width: '420px' }"
      :mask-closable="!muteSubmitting"
    >
      <div class="mute-body">
        <p class="mute-target">{{ t('circle.manage.muteTarget', { name: displayName(muteTarget) }) }}</p>
        <div class="field-label">{{ t('circle.manage.muteDuration') }}</div>
        <div class="mute-presets">
          <NButton
            v-for="p in mutePresets"
            :key="p.hours"
            size="small"
            round
            :type="muteHours === p.hours ? 'primary' : 'default'"
            :secondary="muteHours === p.hours"
            @click="selectMutePreset(p.hours)"
          >
            {{ p.label }}
          </NButton>
        </div>
        <NInputNumber
          v-model:value="muteHours"
          :min="MUTE_DURATION_MIN"
          :max="MUTE_DURATION_MAX"
          :show-button="false"
          class="mute-custom"
        >
          <template #suffix>{{ t('circle.manage.hoursUnit') }}</template>
        </NInputNumber>
      </div>
      <template #footer>
        <div class="mute-footer">
          <NButton quaternary round :disabled="muteSubmitting" @click="muteModalVisible = false">
            {{ t('common.cancel') }}
          </NButton>
          <NButton type="primary" round :loading="muteSubmitting" @click="confirmMute">
            {{ t('common.confirm') }}
          </NButton>
        </div>
      </template>
    </NModal>
  </div>
</template>

<script setup>
import { ref, reactive, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NAvatar, NButton, NTag, NTabs, NTabPane, NSpin, NEmpty,
  NModal, NInputNumber, NIcon, NInput, useMessage, useDialog
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { Search as SearchIcon } from '@vicons/tabler'
import AppDropdown from '@/components/common/AppDropdown.vue'
import AppHeader from '@/components/layout/AppHeader.vue'
import SideNav from '@/components/layout/SideNav.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import CircleAdminHeader from '@/components/circle/CircleAdminHeader.vue'
import {
  getCircleDetail, getCircleMembers, setMemberRole, transferOwnership,
  muteMember, unmuteMember, banMember, unbanMember, removeMember
} from '@/api/circle'
import { useFormatTime } from '@/utils/i18n'
import { useDebounceFn } from '@/utils/throttle'
import { useCircleMeta } from '@/composables/useCircleMeta'
import { usePageTitle } from '@/composables/usePageTitle'
import {
  CIRCLE_ROLE, MEMBER_STATUS, MEMBER_PAGE_SIZE,
  MUTE_DURATION_MIN, MUTE_DURATION_MAX,
  isManager, isOwner, canTarget, canModerate
} from '@/constants/circle'

const route = useRoute()
const router = useRouter()
const message = useMessage()
const dialog = useDialog()
const { t } = useI18n()
const { formatTime } = useFormatTime()
const { getRoleInfo } = useCircleMeta()
const { setTitleData } = usePageTitle()

const offset = ref(260)
const circleId = computed(() => route.params.id)

// 圈子信息与我的角色（权限守卫依据）
const circle = ref({})
const myRole = computed(() => circle.value.member_role || 0)

const pageLoading = ref(true)
const actingId = ref('')

// Tab 定义：key → 后端 status（注意按文档以字符串传参）；默认展示正常成员
const TAB_STATUS = { normal: '1', muted: '2', banned: '3' }
const tabDefs = computed(() => [
  { key: 'normal', label: t('circle.manage.tabs.normal') },
  { key: 'muted', label: t('circle.manage.tabs.muted') },
  { key: 'banned', label: t('circle.manage.tabs.banned') }
])
const activeTab = ref('normal')

// 每个 Tab 独立的游标分页状态（keyword 为该 Tab 的搜索关键字，服务端按用户名/邮箱匹配）
const tabState = reactive({
  normal: { members: [], cursor: '', hasMore: true, loading: false, loaded: false, keyword: '' },
  muted: { members: [], cursor: '', hasMore: true, loading: false, loaded: false, keyword: '' },
  banned: { members: [], cursor: '', hasMore: true, loading: false, loaded: false, keyword: '' }
})

// 加载世代：刷新 Tab 后旧请求的响应直接丢弃，防止覆盖新列表
const genMap = { normal: 0, muted: 0, banned: 0 }

const loadTab = async (key, isRefresh = false) => {
  const state = tabState[key]
  if (!isRefresh && (state.loading || !state.hasMore)) return

  const gen = ++genMap[key]
  state.loading = true
  let succeeded = false
  try {
    const params = {
      circle_id: circleId.value,
      status: TAB_STATUS[key],
      size: MEMBER_PAGE_SIZE
    }
    // 搜索：翻页必须带同一 keyword（与 cursor 配套），关键词变化时由调用方重置 cursor
    if (state.keyword) {
      params.keyword = state.keyword
    }
    if (!isRefresh && state.cursor) {
      params.cursor = state.cursor
    }

    const res = await getCircleMembers(params)
    if (gen !== genMap[key]) return // 过期响应丢弃
    const items = res.data?.members || []
    state.members = isRefresh ? items : [...state.members, ...items]
    // 空字符串游标 = 没有更多页
    state.cursor = res.data?.cursor || ''
    state.hasMore = state.cursor !== ''
    state.loaded = true
    succeeded = true
  } catch (error) {
    if (gen !== genMap[key]) return
    console.error('加载成员列表失败:', error)
    if (error?.code === 210) {
      // 搜索服务不可用（503）
      message.error(t('circle.manage.searchUnavailable'))
    } else {
      message.error(t('circle.manage.loadFailed'))
    }
  } finally {
    if (gen === genMap[key]) {
      state.loading = false
      // 仅成功后才挂观察器：失败时触发器常驻视口会造成无限自动重试
      if (succeeded && state.hasMore) {
        await nextTick()
        ensureObserver(key)
      }
    }
  }
}

// 就地刷新某个 Tab（回到第一页）：先自增世代使在途请求过期
const reloadTab = async (key) => {
  genMap[key]++
  tabState[key].cursor = ''
  tabState[key].hasMore = true
  await loadTab(key, true)
}

// ---------- 搜索（服务端 keyword：按用户名/邮箱匹配，与当前 Tab 的 status 过滤叠加） ----------

// 输入框值；每个 Tab 各自记忆关键字，切 Tab 时回填
const searchInput = ref('')

// 切 Tab：没加载过的 Tab 拉首页（loadTab 会带上该 Tab 的 keyword）
watch(activeTab, (key) => {
  searchInput.value = tabState[key].keyword
  if (!tabState[key].loaded) {
    loadTab(key, true)
  }
})

const debouncedSearch = useDebounceFn((key) => reloadTab(key), 300)

// 关键词变化：同步到当前 Tab 状态并防抖从首页重拉（cursor 重置；翻页时 loadTab 带同一 keyword）
watch(searchInput, (val) => {
  const key = activeTab.value
  const kw = val.trim()
  // Tab 切换回填（或值未变）不触发搜索
  if (tabState[key].keyword === kw) return
  tabState[key].keyword = kw
  if (!kw) {
    reloadTab(key) // 清空立即恢复全量列表，不等防抖
  } else {
    debouncedSearch(key)
  }
})

// 无限滚动：每个 Tab 独立哨兵；隐藏面板内的哨兵不会进入视口，互不干扰
const sentinelEls = {}
const observers = {}

const setSentinel = (key) => (el) => {
  if (el) sentinelEls[key] = el
  else delete sentinelEls[key]
}

const ensureObserver = (key) => {
  if (observers[key] || !sentinelEls[key]) return
  observers[key] = new IntersectionObserver((entries) => {
    if (entries.some(e => e.isIntersecting)) {
      const state = tabState[key]
      if (state.hasMore && !state.loading) {
        loadTab(key)
      }
    }
  }, { rootMargin: '200px' })
  observers[key].observe(sentinelEls[key])
}

const cleanupObservers = () => {
  Object.values(observers).forEach(o => o.disconnect())
  Object.keys(observers).forEach(k => delete observers[k])
}

// ---------- 业务操作 ----------

const displayName = (m) => m?.username || t('circle.manage.unknownUser')

// 禁言剩余时长（后端保证不会出现 status=2 但已过期的脏数据，<=0 仅兜底）
const muteRemaining = (m) => {
  if (!m?.mute_end_time) return ''
  const diff = new Date(m.mute_end_time).getTime() - Date.now()
  if (diff <= 0) return ''
  const minutes = Math.floor(diff / 60000)
  if (minutes < 60) return t('circle.manage.remainingMinutes', { n: minutes })
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return t('circle.manage.remainingHours', { n: hours })
  return t('circle.manage.remainingDays', { n: Math.floor(hours / 24) })
}

// 统一错误处理：按 error.code 分支渲染自有文案，不展示后端英文 message。
// 信封 code 与 HTTP 状态一一对应（201↔400、203↔403、204↔404、207↔409，文档 §四），两者都认。
// 202/401 未登录已由 axios 拦截器全局处理（清 token 跳登录），此处无需关心。
const handleManageError = (error) => {
  const code = error?.code
  const status = error?.status
  if (code === 207 || code === 409 || status === 409) {
    // 状态冲突：目标已被别人改过 / 已是该角色 / 重复操作 → 刷新后让用户重试
    message.warning(t('circle.manage.error.conflict'))
    reloadTab(activeTab.value)
    return
  }
  if (code === 203 || status === 403) {
    // 权限不足：角色可能已变更，重查详情，若已降级则退回圈子详情页
    message.error(t('circle.manage.error.noPermission'))
    refreshRoleAndGuard()
    return
  }
  if (code === 204 || status === 404) {
    message.warning(t('circle.manage.error.memberNotFound'))
    reloadTab(activeTab.value)
    return
  }
  if (code === 201 || status === 400) {
    message.error(t('circle.manage.error.badRequest'))
    return
  }
  message.error(t('circle.manage.error.generic'))
}

const refreshRoleAndGuard = async () => {
  try {
    const res = await getCircleDetail(circleId.value)
    if (res.data) circle.value = res.data
    if (!isManager(circle.value.member_role)) {
      message.warning(t('circle.manage.error.roleDemoted'))
      router.replace(`/circle/${circleId.value}`)
    }
  } catch {
    // 详情拉取失败不打断当前页
  }
}

// 执行一次管理操作：成功后就地刷新当前 Tab
const executeAction = async ({ apiCall, successMsg }) => {
  try {
    await apiCall()
    message.success(successMsg)
    await reloadTab(activeTab.value)
    return true
  } catch (error) {
    handleManageError(error)
    return false
  }
}

// 禁言：弹时长选择
const muteModalVisible = ref(false)
const muteSubmitting = ref(false)
const muteTarget = ref(null)
const muteHours = ref(24)

const mutePresets = computed(() => [
  { hours: 1, label: t('circle.manage.mutePreset1h') },
  { hours: 24, label: t('circle.manage.mutePreset24h') },
  { hours: 168, label: t('circle.manage.mutePreset7d') },
  { hours: 720, label: t('circle.manage.mutePreset30d') }
])

const openMuteModal = (m) => {
  muteTarget.value = m
  muteHours.value = 24
  muteModalVisible.value = true
}

const selectMutePreset = (hours) => {
  muteHours.value = hours
}

const confirmMute = async () => {
  const hours = Number(muteHours.value)
  if (!Number.isInteger(hours) || hours < MUTE_DURATION_MIN || hours > MUTE_DURATION_MAX) {
    message.error(t('circle.manage.muteRangeError'))
    return
  }
  muteSubmitting.value = true
  const ok = await executeAction({
    apiCall: () => muteMember({
      circle_id: circleId.value,
      target_user_id: muteTarget.value.user_id,
      duration_hours: hours
    }),
    successMsg: t('circle.manage.success.muted')
  })
  muteSubmitting.value = false
  if (ok) muteModalVisible.value = false
}

const handleUnmute = (m) => {
  actingId.value = m.user_id
  executeAction({
    apiCall: () => unmuteMember({ circle_id: circleId.value, target_user_id: m.user_id }),
    successMsg: t('circle.manage.success.unmuted')
  }).finally(() => { actingId.value = '' })
}

// 移出：status 1/2 → 4，不拉黑（公开圈可重新加入，私密圈需重新邀请），二次确认
const handleRemove = (m) => {
  dialog.warning({
    title: t('circle.manage.confirm.removeTitle'),
    content: t('circle.manage.confirm.removeContent', { name: displayName(m) }),
    positiveText: t('circle.manage.actions.remove'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      actingId.value = m.user_id
      return executeAction({
        apiCall: () => removeMember({ circle_id: circleId.value, target_user_id: m.user_id }),
        successMsg: t('circle.manage.success.removed')
      }).finally(() => { actingId.value = '' })
    }
  })
}

// 拉黑：移出且解除前不能再加入，二次确认
const handleBan = (m) => {
  dialog.warning({
    title: t('circle.manage.confirm.banTitle'),
    content: t('circle.manage.confirm.banContent', { name: displayName(m) }),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      actingId.value = m.user_id
      return executeAction({
        apiCall: () => banMember({ circle_id: circleId.value, target_user_id: m.user_id }),
        successMsg: t('circle.manage.success.banned')
      }).finally(() => { actingId.value = '' })
    }
  })
}

// 解除拉黑：落为已退出，公开圈可重新加入、私密圈需邀请（文案需说明）
const handleUnban = (m) => {
  dialog.warning({
    title: t('circle.manage.confirm.unbanTitle'),
    content: t('circle.manage.confirm.unbanContent', { name: displayName(m) }),
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: () => {
      actingId.value = m.user_id
      return executeAction({
        apiCall: () => unbanMember({ circle_id: circleId.value, target_user_id: m.user_id }),
        successMsg: t('circle.manage.success.unbanned')
      }).finally(() => { actingId.value = '' })
    }
  })
}

// 圈主专属：正常成员行的三点菜单（任免管理员 / 转让圈主）
const rowMenuOptions = (m) => {
  const options = []
  if (m.role === CIRCLE_ROLE.MEMBER) {
    options.push({ label: t('circle.manage.actions.setAdmin'), key: 'setAdmin' })
  } else if (m.role === CIRCLE_ROLE.ADMIN) {
    options.push({ label: t('circle.manage.actions.removeAdmin'), key: 'removeAdmin' })
  }
  options.push({ label: t('circle.manage.actions.transfer'), key: 'transfer' })
  return options
}

const handleRowMenu = (key, m) => {
  if (key === 'setAdmin') {
    actingId.value = m.user_id
    executeAction({
      apiCall: () => setMemberRole({ circle_id: circleId.value, target_user_id: m.user_id, role: CIRCLE_ROLE.ADMIN }),
      successMsg: t('circle.manage.success.adminSet')
    }).finally(() => { actingId.value = '' })
    return
  }
  if (key === 'removeAdmin') {
    dialog.warning({
      title: t('circle.manage.confirm.removeAdminTitle'),
      content: t('circle.manage.confirm.removeAdminContent', { name: displayName(m) }),
      positiveText: t('common.confirm'),
      negativeText: t('common.cancel'),
      onPositiveClick: () => {
        actingId.value = m.user_id
        return executeAction({
          apiCall: () => setMemberRole({ circle_id: circleId.value, target_user_id: m.user_id, role: CIRCLE_ROLE.MEMBER }),
          successMsg: t('circle.manage.success.adminRemoved')
        }).finally(() => { actingId.value = '' })
      }
    })
    return
  }
  if (key === 'transfer') {
    // 不可逆操作：转让后当前用户立即失去圈主身份，本地角色状态全部失效 → 强确认后退回详情页重拉
    dialog.warning({
      title: t('circle.manage.confirm.transferTitle'),
      content: t('circle.manage.confirm.transferContent', { name: displayName(m) }),
      positiveText: t('common.confirm'),
      negativeText: t('common.cancel'),
      onPositiveClick: async () => {
        actingId.value = m.user_id
        try {
          await transferOwnership({ circle_id: circleId.value, target_user_id: m.user_id })
          message.success(t('circle.manage.success.transferred'))
          router.replace(`/circle/${circleId.value}`)
        } catch (error) {
          handleManageError(error)
        } finally {
          actingId.value = ''
        }
      }
    })
  }
}

// ---------- 初始化 ----------

const initPage = async () => {
  pageLoading.value = true
  try {
    const res = await getCircleDetail(circleId.value)
    circle.value = res.data || {}
    // 页面内自查角色（仿 /admin/agents）：非管理侧一律退回详情页
    if (!isManager(circle.value.member_role)) {
      message.error(t('circle.manage.noPermissionVisit'))
      router.replace(`/circle/${circleId.value}`)
      return
    }
    // 通过角色校验后用圈子名覆盖标签页标题
    if (circle.value.name) {
      setTitleData('title.circleMembersName', { name: circle.value.name })
    }
    activeTab.value = 'normal'
    // 先渲染列表面板（哨兵元素就位），再发首页请求，成功后才能挂上观察器
    pageLoading.value = false
    await nextTick()
    await loadTab('normal', true)
  } catch (error) {
    console.error('加载圈子信息失败:', error)
    message.error(t('messages.getDetailFailed', { error: error.message || t('common.unknownError') }))
    router.replace(`/circle/${circleId.value}`)
  } finally {
    pageLoading.value = false
  }
}

onMounted(initPage)

// 同组件复用（/circle/A/members → /circle/B/members）时整体重置；
// 页头组件随 pageLoading 切换卸载重建，角标由其 onMounted 重新拉取
watch(() => route.params.id, (newId, oldId) => {
  if (!newId || newId === oldId) return
  cleanupObservers()
  circle.value = {}
  Object.keys(tabState).forEach((key) => {
    tabState[key] = { members: [], cursor: '', hasMore: true, loading: false, loaded: false, keyword: '' }
    // 同时作废上一个圈子仍在途的 loadTab，防止旧响应通过世代校验后写回新列表
    genMap[key]++
  })
  searchInput.value = ''
  initPage()
})

onUnmounted(cleanupObservers)
</script>

<style scoped>
.circle-members-page {
  min-height: 100vh;
  position: relative;
}

.main-content {
  margin-top: var(--header-height);
  min-height: calc(100vh - var(--header-height));
  padding: 24px;
  transition: margin-left 0.3s ease, width 0.3s ease;
}

.members-panel {
  max-width: 900px;
  margin: 20px auto 0;
  padding: 4px 20px 24px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid rgba(255, 255, 255, 0.08);
  border-radius: 16px;
}

/* 搜索框：胶囊造型，固定在 tab 行右侧（NTabs suffix 插槽），作用于当前 Tab */
.member-search {
  width: 220px;
  --n-border-radius: 999px !important;
  --n-color: rgba(255, 255, 255, 0.05) !important;
  --n-color-focus: rgba(255, 255, 255, 0.08) !important;
  --n-border: 1px solid rgba(255, 255, 255, 0.1) !important;
  --n-border-hover: 1px solid rgba(102, 234, 194, 0.45) !important;
  --n-border-focus: 1px solid rgba(102, 234, 194, 0.65) !important;
  --n-box-shadow-focus: 0 0 0 3px rgba(102, 234, 194, 0.15) !important;
  --n-caret-color: #66eac2 !important;
}

@media (max-width: 640px) {
  .member-search {
    width: 150px;
  }
}

/* 成员列表 */
.member-list {
  display: flex;
  flex-direction: column;
}

.list-empty {
  padding: 48px 0;
}

.member-row {
  display: flex;
  align-items: center;
  gap: 14px;
  padding: 14px 8px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  transition: background 0.2s cubic-bezier(0.4, 0, 0.2, 1);
  border-radius: 10px;
}

.member-row:hover {
  background: rgba(255, 255, 255, 0.04);
}

.member-main {
  flex: 1;
  min-width: 0;
}

.member-name-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.member-name {
  font-weight: 600;
  color: var(--text-primary, #fff);
  font-size: 0.95rem;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.member-name:hover {
  color: #8af0d0;
}

.member-meta {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 4px;
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.45);
}

.meta-muted {
  color: #ff8181;
}

.row-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.list-footer {
  padding: 14px 0 4px;
  text-align: center;
  font-size: 0.8rem;
  color: rgba(255, 255, 255, 0.35);
}

.loading-state {
  display: flex;
  justify-content: center;
  padding: 16px 0;
}

.load-sentinel {
  height: 1px;
}

/* 骨架屏：页面级骨架占满内容区宽度；列表内骨架铺满面板 */
.page-skeleton {
  max-width: 900px;
  margin: 0 auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.member-skeleton {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.sk-brief {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 16px 20px;
}

.sk-avatar {
  width: 48px;
  height: 48px;
  border-radius: 50%;
}

.sk-lines {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.sk-line {
  height: 14px;
  border-radius: 7px;
}

.sk-line--title {
  width: 30%;
}

.sk-line--sub {
  width: 45%;
}

.sk-tabs {
  height: 40px;
  border-radius: 10px;
  margin-top: 20px;
}

.sk-row {
  height: 68px;
  border-radius: 12px;
}

.sk-avatar,
.sk-line,
.sk-tabs,
.sk-row {
  background: linear-gradient(
    90deg,
    rgba(255, 255, 255, 0.05) 25%,
    rgba(255, 255, 255, 0.1) 50%,
    rgba(255, 255, 255, 0.05) 75%
  );
  background-size: 200% 100%;
  animation: member-shimmer 1.5s ease-in-out infinite;
}

@keyframes member-shimmer {
  0% {
    background-position: 200% 0;
  }
  100% {
    background-position: -200% 0;
  }
}

/* 禁言弹窗内部布局（自身 DOM 带 scoped 属性，scoped 样式可命中；
   弹窗外壳的暗色皮肤由全局 main.css 统一提供） */
.mute-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.mute-target {
  margin: 0;
  color: rgba(255, 255, 255, 0.7);
  font-size: 0.9rem;
}

.field-label {
  font-size: 0.85rem;
  color: rgba(255, 255, 255, 0.6);
}

.mute-presets {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.mute-custom {
  width: 100%;
}

.mute-footer {
  display: flex;
  justify-content: flex-end;
  gap: 12px;
}

/* 移动端：行内操作过窄时换行 */
@media (max-width: 640px) {
  .member-row {
    flex-wrap: wrap;
  }

  .row-actions {
    width: 100%;
    justify-content: flex-end;
  }
}
</style>
