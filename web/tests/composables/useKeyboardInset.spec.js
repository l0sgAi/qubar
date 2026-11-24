import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { ref, nextTick, effectScope } from 'vue'
import { useKeyboardInset } from '@/composables/useKeyboardInset'

// jsdom 没有 visualViewport：用可派发事件的假对象模拟键盘弹出 / 收起
const createViewport = () => {
  const target = new EventTarget()
  target.height = 800
  target.offsetTop = 0
  return target
}

let viewport
let scope

const setViewport = (height, offsetTop = 0, type = 'resize') => {
  viewport.height = height
  viewport.offsetTop = offsetTop
  viewport.dispatchEvent(new Event(type))
}

const run = (active) => {
  scope = effectScope()
  return scope.run(() => useKeyboardInset(active))
}

describe('useKeyboardInset', () => {
  beforeEach(() => {
    viewport = createViewport()
    window.visualViewport = viewport
    window.innerHeight = 800
  })

  afterEach(() => {
    scope?.stop()
    delete window.visualViewport
  })

  it('键盘弹出：遮挡高度 = 布局视口 − 视觉视口高度 − 顶部偏移', () => {
    const { inset } = run(true)
    expect(inset.value).toBe(0)
    setViewport(500)
    expect(inset.value).toBe(300)
    // iOS 把视觉视口往下平移
    setViewport(500, 120, 'scroll')
    expect(inset.value).toBe(180)
    setViewport(800)
    expect(inset.value).toBe(0)
  })

  it('亚像素误差不算键盘', () => {
    const { inset } = run(true)
    setViewport(799.4)
    expect(inset.value).toBe(0)
  })

  it('只在 active 期间监听，关闭后归零', async () => {
    const active = ref(false)
    const { inset } = run(active)
    setViewport(500)
    expect(inset.value).toBe(0)

    active.value = true
    await nextTick()
    expect(inset.value).toBe(300)

    active.value = false
    await nextTick()
    expect(inset.value).toBe(0)
    setViewport(400)
    expect(inset.value).toBe(0)
  })

  it('作用域销毁后不再监听', () => {
    const { inset } = run(true)
    scope.stop()
    setViewport(500)
    expect(inset.value).toBe(0)
  })

  it('不支持 visualViewport 时恒为 0', () => {
    delete window.visualViewport
    const { inset } = run(true)
    expect(inset.value).toBe(0)
  })
})
