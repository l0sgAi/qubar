import { describe, it, expect, vi, beforeEach } from 'vitest'

// 总开关可切换：getter 让每次读取都拿到 flags 的当前值
const flags = vi.hoisted(() => ({ enabled: true }))
vi.mock('@/constants/breakpoints', async (importOriginal) => {
  const mod = await importOriginal()
  return {
    ...mod,
    get MOBILE_SHELL_ENABLED() { return flags.enabled }
  }
})

const { useAppShell, useMobileShell, SIDENAV_WIDTH, SIDENAV_COLLAPSED_WIDTH } = await import('@/composables/useAppShell')
const { __resetBreakpoint } = await import('@/composables/useBreakpoint')
const { setMatchMedia } = await import('../setup')

const MOBILE_PADDING = 'calc(var(--tabbar-height) + var(--safe-bottom) + 16px)'

describe('useAppShell', () => {
  beforeEach(() => {
    __resetBreakpoint()
    flags.enabled = true
  })

  it('桌面：默认让出展开侧栏宽度，与迁移前内联样式一致', () => {
    const { offset, mobileShell, contentStyle } = useAppShell()
    expect(offset.value).toBe(SIDENAV_WIDTH)
    expect(mobileShell.value).toBe(false)
    expect(contentStyle.value).toEqual({ 'margin-left': '260px', width: 'calc(100% - 260px)' })
  })

  it('侧栏收起后偏移跟随', () => {
    const { offset, contentStyle } = useAppShell()
    offset.value = SIDENAV_COLLAPSED_WIDTH
    expect(contentStyle.value).toEqual({ 'margin-left': '64px', width: 'calc(100% - 64px)' })
  })

  it('移动端外框：不偏移，底部让出 TabBar（组件外无路由，按默认 meta）', () => {
    setMatchMedia(true)
    const { mobileShell, contentStyle } = useAppShell()
    expect(mobileShell.value).toBe(true)
    expect(contentStyle.value).toEqual({ 'padding-bottom': MOBILE_PADDING })
  })

  it('跨断点实时切换', () => {
    const { mobileShell, contentStyle } = useAppShell()
    setMatchMedia(true)
    expect(mobileShell.value).toBe(true)
    expect(contentStyle.value).toEqual({ 'padding-bottom': MOBILE_PADDING })
    setMatchMedia(false)
    expect(mobileShell.value).toBe(false)
    expect(contentStyle.value).toEqual({ 'margin-left': '260px', width: 'calc(100% - 260px)' })
  })

  it('总开关关闭时手机断点仍用桌面布局（P0 行为）', () => {
    flags.enabled = false
    setMatchMedia(true)
    const { mobileShell, contentStyle } = useAppShell()
    expect(mobileShell.value).toBe(false)
    expect(contentStyle.value).toEqual({ 'margin-left': '260px', width: 'calc(100% - 260px)' })
    expect(useMobileShell().value).toBe(false)
  })
})
