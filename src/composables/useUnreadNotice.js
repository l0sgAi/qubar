import { onMounted, onBeforeUnmount, readonly } from 'vue'
import { useNoticeStream } from '@/composables/useNoticeStream'

// 未读通知数（模块级单例）。原先连接由 AppHeader 实例持有，移动端外框不渲染 AppHeader，
// 故上提为单例：AppHeader / MobileTabBar 等消费方各自 useUnreadNotice()，共享一条 SSE 连接。
//
// 生命周期（引用计数）：
// - 消费方挂载 → acquire：取消待停止、启动连接（幂等；未登录为空操作）
// - 全部消费方卸载 → 延迟 STOP_GRACE_MS 后 stop：页面切换（旧页卸载 → 新页挂载）期间不断连，
//   避免每次跳转都重建 SSE；离开到无消费方的页面（如登录页）则最终停止
// - 主动登出 → stopUnreadNotice() 立即停止
//
// 私信（后端 v0.9.0）：同一条连接还提供私信未读数 dmUnreadCount（与 unreadCount 独立）和 dm-message 事件，
// 见 useNoticeStream.js。私信页 / 导航角标通过 useUnreadNotice() 或下方的 onDmMessage / refreshDmUnreadCount 使用。

export const STOP_GRACE_MS = 3000

let stream = null
let holders = 0
let stopTimer = null
let listening = false
// auth-expired 回调栈：调用最近挂载的消费方（各消费方行为一致：提示 + 跳登录页）
const authExpiredHandlers = []

const getStream = () => {
  if (!stream) {
    stream = useNoticeStream({
      onAuthExpired: () => {
        const handler = authExpiredHandlers[authExpiredHandlers.length - 1]
        if (handler) handler()
      }
    })
  }
  return stream
}

// 通知页已读操作后的本地校正（read 按条数减、read-all 清零；SSE 全量推送随后覆盖校准）
const onNoticeRead = (e) => {
  const { unreadCount } = getStream()
  unreadCount.value = Math.max(0, unreadCount.value - (e.detail?.count || 0))
}
const onNoticeReadAll = () => {
  getStream().unreadCount.value = 0
}

const listen = () => {
  if (listening) return
  window.addEventListener('notice-read', onNoticeRead)
  window.addEventListener('notice-read-all', onNoticeReadAll)
  listening = true
}

const unlisten = () => {
  if (!listening) return
  window.removeEventListener('notice-read', onNoticeRead)
  window.removeEventListener('notice-read-all', onNoticeReadAll)
  listening = false
}

const clearStopTimer = () => {
  if (stopTimer) {
    clearTimeout(stopTimer)
    stopTimer = null
  }
}

export function acquireUnreadNotice() {
  clearStopTimer()
  holders++
  listen()
  getStream().start()
}

export function releaseUnreadNotice() {
  holders = Math.max(0, holders - 1)
  if (holders === 0) {
    clearStopTimer()
    stopTimer = setTimeout(stopUnreadNotice, STOP_GRACE_MS)
  }
}

/** 立即停止并清零（登出时调用；下次 acquire 会重新建连）。 */
export function stopUnreadNotice() {
  clearStopTimer()
  unlisten()
  if (!stream) return
  stream.stop()
  stream.unreadCount.value = 0
  stream.dmUnreadCount.value = 0
}

/**
 * 注册 dm-message 回调（不随组件生命周期，需自行注销）。组件内优先用 useUnreadNotice({ onDmMessage })。
 * 只注册回调，不会建立连接：连接由导航栏等消费方持有。
 * @param {Function} handler - (msg) => void，msg 见 useNoticeStream 的 onDmMessage
 * @returns {Function} 注销函数
 */
export function onDmMessage(handler) {
  return getStream().onDmMessage(handler)
}

/** 立即拉一次私信未读数（标记已读 / 删除会话后调用；SSE 正常时 dm-unread 约 1s 内也会推新值）。 */
export function refreshDmUnreadCount() {
  return getStream().refreshDmUnread()
}

/**
 * 组件内使用：挂载时 acquire、卸载时 release。
 * @param {Object} [options]
 * @param {Function} [options.onAuthExpired] - 服务端推送登录态失效时的回调（token 已清除）
 * @param {Function} [options.onDmMessage] - 收到 dm-message 时的回调（挂载时注册、卸载时注销）
 * @returns {{ unreadCount: Readonly<Ref<number>>, dmUnreadCount: Readonly<Ref<number>> }}
 */
export function useUnreadNotice({ onAuthExpired, onDmMessage: dmHandler } = {}) {
  const { unreadCount, dmUnreadCount } = getStream()
  let offDm = null

  onMounted(() => {
    if (onAuthExpired) authExpiredHandlers.push(onAuthExpired)
    if (dmHandler) offDm = getStream().onDmMessage(dmHandler)
    acquireUnreadNotice()
  })

  onBeforeUnmount(() => {
    if (onAuthExpired) {
      const i = authExpiredHandlers.lastIndexOf(onAuthExpired)
      if (i !== -1) authExpiredHandlers.splice(i, 1)
    }
    if (offDm) {
      offDm()
      offDm = null
    }
    releaseUnreadNotice()
  })

  return { unreadCount: readonly(unreadCount), dmUnreadCount: readonly(dmUnreadCount) }
}

// 仅供单测：重置单例
export function __resetUnreadNotice() {
  stopUnreadNotice()
  stream = null
  holders = 0
  authExpiredHandlers.length = 0
}
