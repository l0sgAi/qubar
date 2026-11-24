import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'

// 替换底层 SSE：记录 start/stop 调用，暴露 onAuthExpired 以便模拟服务端推送
const fake = vi.hoisted(() => ({ instances: [] }))
vi.mock('@/composables/useNoticeStream', async () => {
  const { ref } = await import('vue')
  return {
    useNoticeStream: (options = {}) => {
      const handlers = new Set()
      const inst = {
        options,
        unreadCount: ref(0),
        dmUnreadCount: ref(0),
        handlers,
        onDmMessage: vi.fn((fn) => {
          handlers.add(fn)
          return () => handlers.delete(fn)
        }),
        refreshDmUnread: vi.fn().mockResolvedValue(),
        start: vi.fn(),
        stop: vi.fn()
      }
      fake.instances.push(inst)
      return inst
    }
  }
})

const {
  useUnreadNotice,
  acquireUnreadNotice,
  releaseUnreadNotice,
  stopUnreadNotice,
  onDmMessage,
  refreshDmUnreadCount,
  __resetUnreadNotice,
  STOP_GRACE_MS
} = await import('@/composables/useUnreadNotice')

const stream = () => fake.instances[fake.instances.length - 1]

// 挂载一个消费方组件，返回 wrapper 与拿到的 unreadCount
const mountConsumer = (options) => {
  const captured = ref(null)
  const Comp = defineComponent({
    setup() {
      captured.value = useUnreadNotice(options).unreadCount
      return () => h('div')
    }
  })
  return { wrapper: mount(Comp), unread: () => captured.value }
}

describe('useUnreadNotice', () => {
  beforeEach(() => {
    __resetUnreadNotice()
    fake.instances.length = 0
  })

  it('多个消费方共享同一条连接与同一个计数', () => {
    const a = mountConsumer()
    const b = mountConsumer()
    expect(fake.instances).toHaveLength(1)
    stream().unreadCount.value = 7
    expect(a.unread().value).toBe(7)
    expect(b.unread().value).toBe(7)
    a.wrapper.unmount()
    b.wrapper.unmount()
  })

  it('挂载即启动连接（幂等由底层保证）', () => {
    const { wrapper } = mountConsumer()
    expect(stream().start).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('返回只读计数', () => {
    // 写只读 ref 时 Vue 会打 warn，属预期，静音
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { wrapper, unread } = mountConsumer()
    unread().value = 5
    expect(stream().unreadCount.value).toBe(0)
    wrapper.unmount()
  })

  it('页面切换（卸载后宽限期内重新挂载）不断连', () => {
    vi.useFakeTimers()
    const first = mountConsumer()
    first.wrapper.unmount()
    vi.advanceTimersByTime(STOP_GRACE_MS - 1)
    const second = mountConsumer()
    vi.advanceTimersByTime(STOP_GRACE_MS * 2)
    expect(stream().stop).not.toHaveBeenCalled()
    second.wrapper.unmount()
  })

  it('全部卸载超过宽限期后停止并清零', () => {
    vi.useFakeTimers()
    const { wrapper } = mountConsumer()
    stream().unreadCount.value = 3
    wrapper.unmount()
    vi.advanceTimersByTime(STOP_GRACE_MS)
    expect(stream().stop).toHaveBeenCalledTimes(1)
    expect(stream().unreadCount.value).toBe(0)
  })

  it('仍有消费方时不停止', () => {
    vi.useFakeTimers()
    const a = mountConsumer()
    const b = mountConsumer()
    a.wrapper.unmount()
    vi.advanceTimersByTime(STOP_GRACE_MS * 2)
    expect(stream().stop).not.toHaveBeenCalled()
    b.wrapper.unmount()
  })

  it('release 次数多于 acquire 不会变成负数', () => {
    vi.useFakeTimers()
    releaseUnreadNotice()
    releaseUnreadNotice()
    acquireUnreadNotice()
    vi.advanceTimersByTime(STOP_GRACE_MS * 2)
    expect(stream().stop).not.toHaveBeenCalled()
  })

  it('stopUnreadNotice 立即停止（登出）', () => {
    const { wrapper } = mountConsumer()
    stream().unreadCount.value = 4
    stopUnreadNotice()
    expect(stream().stop).toHaveBeenCalledTimes(1)
    expect(stream().unreadCount.value).toBe(0)
    wrapper.unmount()
  })

  it('stop 后再次 acquire 会重新启动', () => {
    acquireUnreadNotice()
    stopUnreadNotice()
    acquireUnreadNotice()
    expect(stream().start).toHaveBeenCalledTimes(2)
  })

  it('notice-read 按条数扣减，不出现负数', () => {
    const { wrapper } = mountConsumer()
    stream().unreadCount.value = 3
    window.dispatchEvent(new CustomEvent('notice-read', { detail: { count: 2 } }))
    expect(stream().unreadCount.value).toBe(1)
    window.dispatchEvent(new CustomEvent('notice-read', { detail: { count: 5 } }))
    expect(stream().unreadCount.value).toBe(0)
    window.dispatchEvent(new CustomEvent('notice-read'))
    expect(stream().unreadCount.value).toBe(0)
    wrapper.unmount()
  })

  it('notice-read-all 清零', () => {
    const { wrapper } = mountConsumer()
    stream().unreadCount.value = 9
    window.dispatchEvent(new CustomEvent('notice-read-all'))
    expect(stream().unreadCount.value).toBe(0)
    wrapper.unmount()
  })

  it('停止后不再响应本地校正事件', () => {
    const { wrapper } = mountConsumer()
    stopUnreadNotice()
    stream().unreadCount.value = 2
    window.dispatchEvent(new CustomEvent('notice-read-all'))
    expect(stream().unreadCount.value).toBe(2)
    wrapper.unmount()
  })

  it('auth-expired 调用最近挂载的消费方回调，卸载后移除', () => {
    const first = vi.fn()
    const second = vi.fn()
    const a = mountConsumer({ onAuthExpired: first })
    const b = mountConsumer({ onAuthExpired: second })

    stream().options.onAuthExpired()
    expect(second).toHaveBeenCalledTimes(1)
    expect(first).not.toHaveBeenCalled()

    b.wrapper.unmount()
    stream().options.onAuthExpired()
    expect(first).toHaveBeenCalledTimes(1)

    a.wrapper.unmount()
    expect(() => stream().options.onAuthExpired()).not.toThrow()
  })

  describe('私信（v0.9.0）', () => {
    const mountDmConsumer = (options) => {
      const captured = {}
      const Comp = defineComponent({
        setup() {
          Object.assign(captured, useUnreadNotice(options))
          return () => h('div')
        }
      })
      return { wrapper: mount(Comp), captured }
    }

    it('dmUnreadCount 共享且只读，与 unreadCount 独立', () => {
      vi.spyOn(console, 'warn').mockImplementation(() => {})
      const a = mountDmConsumer()
      const b = mountDmConsumer()
      stream().dmUnreadCount.value = 5
      expect(a.captured.dmUnreadCount.value).toBe(5)
      expect(b.captured.dmUnreadCount.value).toBe(5)
      expect(a.captured.unreadCount.value).toBe(0)
      a.captured.dmUnreadCount.value = 1
      expect(stream().dmUnreadCount.value).toBe(5)
      a.wrapper.unmount()
      b.wrapper.unmount()
    })

    it('onDmMessage 选项：挂载注册、卸载注销', () => {
      const handler = vi.fn()
      const { wrapper } = mountDmConsumer({ onDmMessage: handler })
      expect(stream().handlers.has(handler)).toBe(true)
      wrapper.unmount()
      expect(stream().handlers.has(handler)).toBe(false)
    })

    it('模块级 onDmMessage 返回注销函数，refreshDmUnreadCount 透传', async () => {
      const handler = vi.fn()
      const off = onDmMessage(handler)
      expect(stream().handlers.has(handler)).toBe(true)
      off()
      expect(stream().handlers.has(handler)).toBe(false)
      await refreshDmUnreadCount()
      expect(stream().refreshDmUnread).toHaveBeenCalledTimes(1)
    })

    it('stopUnreadNotice 同时清零私信未读', () => {
      const { wrapper } = mountDmConsumer()
      stream().dmUnreadCount.value = 3
      stopUnreadNotice()
      expect(stream().dmUnreadCount.value).toBe(0)
      wrapper.unmount()
    })
  })
})
