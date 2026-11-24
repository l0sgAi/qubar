import { ref, readonly } from 'vue'
import { MOBILE_QUERY } from '@/constants/breakpoints'

// 模块级单例：全应用共享一个 matchMedia 监听器，只在跨越断点时触发（无 resize 开销）。
// 首次调用同步读取 matches，首屏不会先渲染桌面再闪回移动端。
const isMobile = ref(false)
let mql = null

const onChange = (e) => {
  isMobile.value = e.matches
}

const ensureListener = () => {
  if (mql || typeof window === 'undefined' || !window.matchMedia) return
  mql = window.matchMedia(MOBILE_QUERY)
  isMobile.value = mql.matches
  mql.addEventListener('change', onChange)
}

export function useBreakpoint() {
  ensureListener()
  return { isMobile: readonly(isMobile) }
}

// 仅供单测：重置单例，下一次调用重新读取 matchMedia
export function __resetBreakpoint() {
  mql?.removeEventListener('change', onChange)
  mql = null
  isMobile.value = false
}
