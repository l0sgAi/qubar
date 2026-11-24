import { describe, it, expect, vi, beforeEach } from 'vitest'

const api = vi.hoisted(() => ({ getUnreadCount: null, getDmUnreadCount: null }))
vi.mock('@/api/notice', () => ({
  getUnreadCount: (...args) => api.getUnreadCount(...args)
}))
vi.mock('@/api/message', () => ({
  getDmUnreadCount: (...args) => api.getDmUnreadCount(...args)
}))

const { useNoticeStream } = await import('@/composables/useNoticeStream')

const POLL_INTERVAL = 30000

// 最小 EventSource 替身：记录实例，可手动派发命名事件 / onopen / onerror
class FakeEventSource {
  static CONNECTING = 0
  static OPEN = 1
  static CLOSED = 2
  static instances = []

  constructor(url) {
    this.url = url
    this.readyState = FakeEventSource.CONNECTING
    this.listeners = {}
    this.onopen = null
    this.onerror = null
    this.close = vi.fn(() => { this.readyState = FakeEventSource.CLOSED })
    FakeEventSource.instances.push(this)
  }

  addEventListener(type, fn) {
    (this.listeners[type] ||= []).push(fn)
  }

  emit(type, data) {
    (this.listeners[type] || []).forEach(fn => fn({ data }))
  }
}

const lastEs = () => FakeEventSource.instances[FakeEventSource.instances.length - 1]

const login = () => {
  localStorage.setItem('qubar_token', 'tok-123')
  localStorage.setItem('qubar_token_expire', String(Date.now() + 3600_000))
}

describe('composables/useNoticeStream', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    FakeEventSource.instances = []
    vi.stubGlobal('EventSource', FakeEventSource)
    api.getUnreadCount = vi.fn().mockResolvedValue({ data: { unread_count: 7 } })
    api.getDmUnreadCount = vi.fn().mockResolvedValue({ data: { unread_count: 2 } })
  })

  it('未登录时 start 为空操作', () => {
    const { start } = useNoticeStream()
    start()
    expect(FakeEventSource.instances).toHaveLength(0)
    expect(api.getUnreadCount).not.toHaveBeenCalled()
  })

  it('登录后建立 SSE（token 走 query），unread-count 全量替换，坏帧忽略', () => {
    login()
    const { start, unreadCount } = useNoticeStream()
    start()
    start() // 幂等
    expect(FakeEventSource.instances).toHaveLength(1)
    expect(lastEs().url).toContain('/notice/stream?satoken=tok-123')

    lastEs().emit('unread-count', JSON.stringify({ unread_count: 3 }))
    expect(unreadCount.value).toBe(3)
    lastEs().emit('unread-count', '{bad json')
    expect(unreadCount.value).toBe(3)
    lastEs().emit('unread-count', JSON.stringify({}))
    expect(unreadCount.value).toBe(0)
  })

  it('conn-evicted：主动 close 阻止自动重连，回落 30s 轮询，不再重建 SSE', async () => {
    login()
    const { start, unreadCount } = useNoticeStream()
    start()
    const es = lastEs()

    es.emit('conn-evicted')
    expect(es.close).toHaveBeenCalledTimes(1)
    expect(es.readyState).toBe(FakeEventSource.CLOSED)

    // 立即拉一次，之后每 30s 一次
    await vi.advanceTimersByTimeAsync(0)
    expect(api.getUnreadCount).toHaveBeenCalledTimes(1)
    expect(unreadCount.value).toBe(7)
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL * 2)
    expect(api.getUnreadCount).toHaveBeenCalledTimes(3)

    // 被挤掉后 start 仍幂等：不会新建连接去挤别的标签页
    start()
    expect(FakeEventSource.instances).toHaveLength(1)
  })

  it('网络抖动（CONNECTING）交给浏览器自动重连，不关连接、不开轮询', async () => {
    login()
    const { start } = useNoticeStream()
    start()
    const es = lastEs()
    es.readyState = FakeEventSource.CONNECTING
    es.onerror()
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL)
    expect(es.close).not.toHaveBeenCalled()
    expect(api.getUnreadCount).not.toHaveBeenCalled()
  })

  it('建连彻底失败（CLOSED）回落轮询；旧 onopen 不能停止降级', async () => {
    login()
    const { start } = useNoticeStream()
    start()
    const es = lastEs()
    es.readyState = FakeEventSource.CLOSED
    es.onerror()
    await vi.advanceTimersByTimeAsync(0)
    expect(api.getUnreadCount).toHaveBeenCalledTimes(1)

    es.onopen()
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL * 2)
    expect(api.getUnreadCount).toHaveBeenCalledTimes(3)
  })

  it('首次 open 不同步；抖动后只发一次重连信号，相同未读数也能驱动校正', () => {
    login()
    const { start, dmSyncVersion, dmUnreadCount } = useNoticeStream()
    start()
    const es = lastEs()
    es.onopen()
    es.emit('dm-unread', JSON.stringify({ unread_count: 2 }))
    expect(dmSyncVersion.value).toBe(0)
    es.onerror()
    es.onerror()
    es.onopen()
    es.onopen()
    es.emit('dm-unread', JSON.stringify({ unread_count: 2 }))
    expect(dmSyncVersion.value).toBe(1)
    expect(dmUnreadCount.value).toBe(2)
  })

  it('轮询每轮同步（同值也触发）；旧 HTTP 与已关闭连接的事件不能覆盖新值', async () => {
    login()
    const { start, stop, dmSyncVersion, dmUnreadCount, unreadCount } = useNoticeStream()
    start()
    const old = lastEs()
    old.emit('conn-evicted')
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL)
    expect(dmSyncVersion.value).toBe(2)
    let lateDm, lateNotice
    api.getDmUnreadCount.mockImplementationOnce(() => new Promise(resolve => { lateDm = resolve }))
    api.getUnreadCount.mockImplementationOnce(() => new Promise(resolve => { lateNotice = resolve }))
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL)
    stop()
    start()
    lastEs().emit('dm-unread', '{"unread_count":3}')
    lastEs().emit('unread-count', '{"unread_count":8}')
    old.emit('auth-expired')
    old.onerror()
    old.onopen()
    lateDm({ data: { unread_count: 99 } })
    lateNotice({ data: { unread_count: 100 } })
    await vi.advanceTimersByTimeAsync(0)
    expect([dmUnreadCount.value, unreadCount.value, dmSyncVersion.value]).toEqual([3, 8, 2])
    expect(lastEs().close).not.toHaveBeenCalled()
  })

  it('auth-expired：close、清 token、回调调用方', () => {
    login()
    const onAuthExpired = vi.fn()
    const { start } = useNoticeStream({ onAuthExpired })
    start()
    const es = lastEs()
    es.emit('auth-expired')
    expect(es.close).toHaveBeenCalled()
    expect(localStorage.getItem('qubar_token')).toBeNull()
    expect(onAuthExpired).toHaveBeenCalledTimes(1)
  })

  it('轮询中登录态失效则跳过请求；请求失败只记日志', async () => {
    login()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    api.getUnreadCount = vi.fn().mockRejectedValue(new Error('boom'))
    const { start } = useNoticeStream()
    start()
    lastEs().emit('conn-evicted')
    await vi.advanceTimersByTimeAsync(0)
    expect(console.error).toHaveBeenCalled()

    localStorage.removeItem('qubar_token')
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL)
    expect(api.getUnreadCount).toHaveBeenCalledTimes(1)
  })

  it('stop 关闭连接并停止轮询', async () => {
    login()
    const { start, stop } = useNoticeStream()
    start()
    const es = lastEs()
    stop()
    expect(es.close).toHaveBeenCalled()

    start()
    lastEs().emit('conn-evicted')
    await vi.advanceTimersByTimeAsync(0)
    stop()
    await vi.advanceTimersByTimeAsync(POLL_INTERVAL * 2)
    expect(api.getUnreadCount).toHaveBeenCalledTimes(1)
  })

  describe('私信事件（v0.9.0 dm-unread / dm-message）', () => {
    const DM = {
      id: '019a7b10-2c3d-7e4f-8a9b-0c1d2e3f4a5b',
      conversation_id: '019a7b10-2c3a-7d11-9e22-3f4a5b6c7d8e',
      sender_id: 'u2',
      sender: { id: 'u2', username: 'alice', avatar_url: '' },
      msg_type: 1,
      content: '你好',
      truncated: false,
      create_time: '2026-11-11T14:30:00+08:00'
    }

    it('dm-unread 全量替换 dmUnreadCount，与 unreadCount 互不覆盖；坏帧忽略', () => {
      login()
      const { start, unreadCount, dmUnreadCount } = useNoticeStream()
      start()
      lastEs().emit('unread-count', JSON.stringify({ unread_count: 3 }))
      lastEs().emit('dm-unread', JSON.stringify({ unread_count: 5 }))
      expect(unreadCount.value).toBe(3)
      expect(dmUnreadCount.value).toBe(5)
      lastEs().emit('dm-unread', 'oops')
      expect(dmUnreadCount.value).toBe(5)
      lastEs().emit('dm-unread', JSON.stringify({}))
      expect(dmUnreadCount.value).toBe(0)
      expect(unreadCount.value).toBe(3)
    })

    it('dm-message 分发给全部回调；注销后不再收到；坏帧 / 缺 conversation_id 忽略', () => {
      login()
      const { start, onDmMessage } = useNoticeStream()
      start()
      const a = vi.fn()
      const b = vi.fn()
      const offA = onDmMessage(a)
      onDmMessage(b)

      lastEs().emit('dm-message', JSON.stringify(DM))
      expect(a).toHaveBeenCalledWith(DM)
      expect(b).toHaveBeenCalledTimes(1)

      offA()
      lastEs().emit('dm-message', JSON.stringify({ ...DM, id: 'x2' }))
      expect(a).toHaveBeenCalledTimes(1)
      expect(b).toHaveBeenCalledTimes(2)

      lastEs().emit('dm-message', '{bad')
      lastEs().emit('dm-message', JSON.stringify({ id: 'x3' }))
      lastEs().emit('dm-message', 'null')
      expect(b).toHaveBeenCalledTimes(2)
    })

    it('回调抛错不影响其它回调；回调里注销自己不影响本轮分发；非函数注册为空操作', () => {
      login()
      vi.spyOn(console, 'error').mockImplementation(() => {})
      const { start, onDmMessage } = useNoticeStream()
      start()
      const bad = vi.fn(() => { throw new Error('boom') })
      let offSelf = null
      const self = vi.fn(() => offSelf())
      const good = vi.fn()
      onDmMessage(bad)
      offSelf = onDmMessage(self)
      onDmMessage(good)
      expect(onDmMessage('not a function')).toBeTypeOf('function')
      onDmMessage('not a function')()

      lastEs().emit('dm-message', JSON.stringify(DM))
      expect(good).toHaveBeenCalledTimes(1)
      expect(self).toHaveBeenCalledTimes(1)
      expect(console.error).toHaveBeenCalled()

      lastEs().emit('dm-message', JSON.stringify(DM))
      expect(self).toHaveBeenCalledTimes(1)
      expect(good).toHaveBeenCalledTimes(2)
    })

    it('回调在 stop / 重新 start 之间保留（单例连接重建后仍生效）', () => {
      login()
      const { start, stop, onDmMessage } = useNoticeStream()
      const handler = vi.fn()
      onDmMessage(handler)
      start()
      stop()
      start()
      lastEs().emit('dm-message', JSON.stringify(DM))
      expect(handler).toHaveBeenCalledTimes(1)
    })

    it('SSE 不可用时轮询同时拉私信未读数（30s）', async () => {
      login()
      const { start, unreadCount, dmUnreadCount } = useNoticeStream()
      start()
      lastEs().readyState = FakeEventSource.CLOSED
      lastEs().onerror()
      await vi.advanceTimersByTimeAsync(0)
      expect(api.getDmUnreadCount).toHaveBeenCalledTimes(1)
      expect(unreadCount.value).toBe(7)
      expect(dmUnreadCount.value).toBe(2)

      api.getDmUnreadCount.mockResolvedValue({ data: { unread_count: 4 } })
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL)
      expect(api.getDmUnreadCount).toHaveBeenCalledTimes(2)
      expect(dmUnreadCount.value).toBe(4)
    })

    it('轮询时一个计数失败不影响另一个；响应缺 data 时保持原值', async () => {
      login()
      vi.spyOn(console, 'error').mockImplementation(() => {})
      api.getUnreadCount = vi.fn().mockRejectedValue(new Error('notice down'))
      const { start, dmUnreadCount } = useNoticeStream()
      start()
      lastEs().emit('conn-evicted')
      await vi.advanceTimersByTimeAsync(0)
      expect(dmUnreadCount.value).toBe(2)

      api.getUnreadCount = vi.fn().mockResolvedValue({ data: { unread_count: 1 } })
      api.getDmUnreadCount = vi.fn().mockRejectedValue(new Error('dm down'))
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL)
      expect(dmUnreadCount.value).toBe(2)
      expect(console.error).toHaveBeenCalledWith('获取私信未读数失败:', expect.any(Error))

      api.getDmUnreadCount = vi.fn().mockResolvedValue({})
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL)
      expect(dmUnreadCount.value).toBe(2)
      api.getDmUnreadCount = vi.fn().mockResolvedValue({ data: {} })
      await vi.advanceTimersByTimeAsync(POLL_INTERVAL)
      expect(dmUnreadCount.value).toBe(0)
    })

    it('refreshDmUnread：登录时拉一次，未登录为空操作', async () => {
      const { refreshDmUnread, dmUnreadCount } = useNoticeStream()
      await refreshDmUnread()
      expect(api.getDmUnreadCount).not.toHaveBeenCalled()
      login()
      await refreshDmUnread()
      expect(dmUnreadCount.value).toBe(2)
    })
  })
})
