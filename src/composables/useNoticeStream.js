import { ref } from 'vue'
import { auth } from '@/utils/auth'
import { getUnreadCount } from '@/api/notice'
import { getDmUnreadCount } from '@/api/message'
import { API_BASE } from '@/config'

// 轮询降级间隔（对接文档建议 ≥30s）
const POLL_INTERVAL = 30000

/**
 * 未读通知数实时推送（SSE 主通道 + 30s 轮询降级）。
 * 对接文档：docs/notice-stream-frontend-api.md（后端 qubar 仓）。
 *
 * 行为要点：
 * - 建连即推全量未读数，之后未读数变化才推；收到直接全量替换，不做加减
 * - 断线由浏览器 EventSource 按服务端 retry:5000 自动重连，不自建重连逻辑
 * - 建连被拒（401 / 服务关停 503）→ readyState=CLOSED → 回落 30s 轮询
 *   （EventSource 拿不到 HTTP 状态码；若真 401，轮询请求会被 axios 拦截器清 token 跳登录）
 * - 收到 conn-evicted（同账号连接数超过上限 5，本连接是最旧的，被新连接挤掉）
 *   → 主动 close（防自动重连又挤掉别的标签页，循环互踢）→ 本标签页回落 30s 轮询
 * - 收到 auth-expired（登出/过期/被踢下线）→ 主动 close（防自动重连 401 死循环）
 *   → 清 token → 回调 onAuthExpired 由调用方跳转登录页
 *
 * 私信（后端 v0.9.0 #241，docs/api/message-api.md §3）复用同一条连接：
 * - dm-unread：私信未读总数全量（首包紧跟 unread-count），直接替换 dmUnreadCount；与 unreadCount 互不覆盖
 * - dm-message：新私信摘要（只推给接收方，content 前 100 字，truncated 标记截断），分发给 onDmMessage 注册的回调；
 *   不保证送达，断线 / 轮询期间收不到，重连后以拉取为准
 * - 轮询降级时同时轮询 GET /message/unread-count（同为 30s）
 *
 * @param {Object} [options]
 * @param {Function} [options.onAuthExpired] - 登录态失效回调（清 token 之后调用）
 * @returns {{ unreadCount: Ref<number>, dmUnreadCount: Ref<number>, onDmMessage: Function,
 *   refreshDmUnread: Function, start: Function, stop: Function }}
 */
export function useNoticeStream({ onAuthExpired } = {}) {
  const unreadCount = ref(0)
  const dmUnreadCount = ref(0)
  const dmMessageHandlers = new Set()
  let es = null
  let pollingTimer = null

  const fetchNoticeUnread = async () => {
    try {
      const res = await getUnreadCount()
      if (res.data) {
        unreadCount.value = res.data.unread_count || 0
      }
    } catch (error) {
      console.error('获取未读通知数失败:', error)
    }
  }

  const fetchDmUnread = async () => {
    try {
      const res = await getDmUnreadCount()
      if (res.data) {
        dmUnreadCount.value = res.data.unread_count || 0
      }
    } catch (error) {
      console.error('获取私信未读数失败:', error)
    }
  }

  // 两个计数互不影响：一个失败不阻塞另一个
  const fetchOnce = async () => {
    if (!auth.isAuthenticated()) return
    await Promise.all([fetchNoticeUnread(), fetchDmUnread()])
  }

  /** 立即拉一次私信未读数（标记已读 / 删除会话后、SSE 不可用时校正角标用）。未登录为空操作。 */
  const refreshDmUnread = async () => {
    if (!auth.isAuthenticated()) return
    await fetchDmUnread()
  }

  /**
   * 注册 dm-message 回调，返回注销函数。回调参数为解析后的事件数据：
   * { id, conversation_id, sender_id, sender: {id, username, avatar_url} | null, msg_type, content, truncated, create_time }
   * 回调抛错不影响其它回调。
   */
  const onDmMessage = (handler) => {
    if (typeof handler !== 'function') return () => {}
    dmMessageHandlers.add(handler)
    return () => dmMessageHandlers.delete(handler)
  }

  const emitDmMessage = (msg) => {
    // 复制一份：回调里注销自己不影响本轮分发
    for (const handler of [...dmMessageHandlers]) {
      try {
        handler(msg)
      } catch (error) {
        console.error('dm-message 回调出错:', error)
      }
    }
  }

  const startPolling = () => {
    if (pollingTimer) return
    pollingTimer = setInterval(fetchOnce, POLL_INTERVAL)
    fetchOnce()
  }

  const stopPolling = () => {
    if (pollingTimer) {
      clearInterval(pollingTimer)
      pollingTimer = null
    }
  }

  const closeEs = () => {
    if (es) {
      es.close()
      es = null
    }
  }

  const connect = () => {
    const token = auth.getToken()
    if (!token) {
      startPolling()
      return
    }
    // EventSource 不能自定义 header，token 走 query（后端 header→query 兜底鉴权）
    es = new EventSource(`${API_BASE}/notice/stream?satoken=${encodeURIComponent(token)}`)

    es.addEventListener('unread-count', (e) => {
      try {
        const data = JSON.parse(e.data)
        unreadCount.value = data.unread_count ?? 0
      } catch {
        // 坏帧忽略，下一帧覆盖
      }
    })

    es.addEventListener('dm-unread', (e) => {
      try {
        const data = JSON.parse(e.data)
        dmUnreadCount.value = data.unread_count ?? 0
      } catch {
        // 坏帧忽略，下一帧覆盖
      }
    })

    es.addEventListener('dm-message', (e) => {
      let msg
      try {
        msg = JSON.parse(e.data)
      } catch {
        return // 坏帧忽略：不保证送达，页面以拉取为准
      }
      if (msg && msg.conversation_id) emitDmMessage(msg)
    })

    es.addEventListener('auth-expired', () => {
      closeEs()
      auth.clearToken()
      if (onAuthExpired) onAuthExpired()
    })

    es.addEventListener('conn-evicted', () => {
      closeEs()
      startPolling()
    })

    // 建连成功 → 停轮询（两通道互备不互斥）
    es.onopen = () => stopPolling()

    es.onerror = () => {
      // CONNECTING：网络抖动，浏览器自动重连中，不干预
      if (es && es.readyState === EventSource.CLOSED) {
        closeEs()
        startPolling()
      }
    }
  }

  /** 启动（幂等）。未登录为空操作。 */
  const start = () => {
    if (es || pollingTimer) return
    if (!auth.isAuthenticated()) return
    connect()
  }

  /** 停止并释放（组件卸载/登出时调用）。 */
  const stop = () => {
    closeEs()
    stopPolling()
  }

  return { unreadCount, dmUnreadCount, onDmMessage, refreshDmUnread, start, stop }
}
