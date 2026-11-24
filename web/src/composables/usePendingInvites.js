import { getCurrentScope, ref, watch } from 'vue'
import { getMyInvites } from '@/api/circleInvite'
import { auth } from '@/utils/auth'

// 侧栏、TabBar 和个人中心复用待接受邀请数。普通挂载最多每分钟请求一次。
const REFRESH_INTERVAL = 60 * 1000
const pendingCount = ref(0)
let cacheSession = null
let lastFetched = null
let inflight = null
let readVersion = 0

export const getInviteSession = () => auth.isAuthenticated() ? auth.getToken() : null

function syncSession() {
  const session = getInviteSession()
  if (session !== cacheSession) {
    cacheSession = session
    readVersion++
    pendingCount.value = 0
    lastFetched = null
    inflight = null
  }
  return session
}

/** 角标请求和「我的邀请」列表共用读序号，迟到的旧总数不能覆盖新读。 */
export function beginPendingInvitesRead() {
  syncSession()
  lastFetched = null
  return ++readVersion
}

export function setPendingInvites(count, version) {
  if (!syncSession() || version !== readVersion) return
  pendingCount.value = Math.max(0, Number(count) || 0)
  lastFetched = Date.now()
}

/** 未登录时归零。读取失败时保留当前账号上一次成功的值，下次挂载可重试。 */
export async function refreshPendingInvites({ force = false } = {}) {
  if (!syncSession()) return 0
  if (!force && inflight) return inflight
  if (!force && lastFetched !== null && Date.now() - lastFetched < REFRESH_INTERVAL) return pendingCount.value

  const version = beginPendingInvitesRead()
  const request = getMyInvites({ page: 1, size: 1 })
    .then((res) => setPendingInvites(res.total, version))
    .catch(() => {})
    .then(() => {
      syncSession()
      return pendingCount.value
    })
  inflight = request
  try {
    return await request
  } finally {
    if (inflight === request) inflight = null
  }
}

export function usePendingInvites() {
  syncSession()
  if (getCurrentScope()) {
    watch(getInviteSession, () => refreshPendingInvites(), { flush: 'sync' })
  }
  return { pendingCount, refreshPendingInvites, beginPendingInvitesRead, setPendingInvites }
}

// 仅供单测重置模块状态
export function __resetPendingInvites() {
  cacheSession = null
  pendingCount.value = 0
  lastFetched = null
  inflight = null
  readVersion++
}
