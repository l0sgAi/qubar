import { afterEach, vi } from 'vitest'

// jsdom 未实现 matchMedia：提供可控 stub，默认桌面（matches=false）。
// 用例里 setMatchMedia(true) 模拟跨越断点（同步触发已注册的 change 监听）。
// 用普通函数而非 vi.fn，避免被 restoreMocks 清掉实现
const mediaState = { matches: false, listeners: new Set() }

window.matchMedia = (query) => ({
  media: query,
  get matches() { return mediaState.matches },
  addEventListener: (type, fn) => { if (type === 'change') mediaState.listeners.add(fn) },
  removeEventListener: (type, fn) => { if (type === 'change') mediaState.listeners.delete(fn) },
  // 旧 API，部分第三方库仍在用
  addListener: (fn) => mediaState.listeners.add(fn),
  removeListener: (fn) => mediaState.listeners.delete(fn),
  onchange: null,
  dispatchEvent: () => false
})

export const setMatchMedia = (matches) => {
  mediaState.matches = matches
  mediaState.listeners.forEach(fn => fn({ matches, media: '' }))
}

export const mediaListenerCount = () => mediaState.listeners.size

afterEach(() => {
  localStorage.clear()
  sessionStorage.clear()
  vi.useRealTimers()
  mediaState.matches = false
  mediaState.listeners.clear()
})
