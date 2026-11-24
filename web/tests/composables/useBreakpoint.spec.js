import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useBreakpoint, __resetBreakpoint } from '@/composables/useBreakpoint'
import { MOBILE_QUERY } from '@/constants/breakpoints'
import { setMatchMedia, mediaListenerCount } from '../setup'

describe('useBreakpoint', () => {
  beforeEach(() => {
    __resetBreakpoint()
  })

  it('断点查询为 ≤ 768px', () => {
    expect(MOBILE_QUERY).toBe('(max-width: 768px)')
  })

  it('初始值同步取自 matchMedia().matches', () => {
    setMatchMedia(true)
    const { isMobile } = useBreakpoint()
    expect(isMobile.value).toBe(true)
  })

  it('默认桌面', () => {
    expect(useBreakpoint().isMobile.value).toBe(false)
  })

  it('跨越断点时随 change 更新', () => {
    const { isMobile } = useBreakpoint()
    setMatchMedia(true)
    expect(isMobile.value).toBe(true)
    setMatchMedia(false)
    expect(isMobile.value).toBe(false)
  })

  it('多次调用共享单例，只注册一个监听器', () => {
    const a = useBreakpoint()
    const b = useBreakpoint()
    expect(mediaListenerCount()).toBe(1)
    setMatchMedia(true)
    expect(a.isMobile.value).toBe(true)
    expect(b.isMobile.value).toBe(true)
  })

  it('返回只读 ref，外部无法改写', () => {
    // 写只读 ref 时 Vue 会打 warn，属预期，静音
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const { isMobile } = useBreakpoint()
    isMobile.value = true
    expect(isMobile.value).toBe(false)
  })

  it('无 matchMedia 环境不抛错，视为桌面', () => {
    const original = window.matchMedia
    window.matchMedia = undefined
    try {
      expect(useBreakpoint().isMobile.value).toBe(false)
    } finally {
      window.matchMedia = original
    }
  })

  it('reset 后注销监听并重新读取', () => {
    useBreakpoint()
    __resetBreakpoint()
    expect(mediaListenerCount()).toBe(0)
    setMatchMedia(true)
    expect(useBreakpoint().isMobile.value).toBe(true)
  })
})
