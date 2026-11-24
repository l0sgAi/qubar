import { ref, watch, toValue, onScopeDispose } from 'vue'

/**
 * 软键盘遮住布局视口底部的高度（px），供底部固定浮层（如移动端评论输入抽屉）抬高到键盘上方。
 *
 * iOS Safari 与 Android Chrome（默认 interactive-widget=resizes-visual）弹出键盘时只缩小视觉视口，
 * position: fixed; bottom: 0 的元素仍贴着布局视口底部，被键盘盖住。
 * 遮挡高度 = 布局视口高度 − 视觉视口高度 − 视觉视口顶部偏移（iOS 会把视觉视口往下平移）。
 * 横竖屏切换、键盘高度变化（联想栏展开）都会触发 visualViewport 的 resize。
 *
 * @param {MaybeRefOrGetter<boolean>} active - 只在浮层打开期间监听，关闭后归零
 * @returns {{ inset: Ref<number> }}
 */
export function useKeyboardInset(active = true) {
  const inset = ref(0)
  const viewport = typeof window !== 'undefined' ? window.visualViewport : null

  const update = () => {
    const overlap = window.innerHeight - viewport.height - viewport.offsetTop
    // 亚像素误差 / 地址栏收放不算键盘
    inset.value = overlap > 1 ? Math.round(overlap) : 0
  }

  let listening = false
  const start = () => {
    if (!viewport || listening) return
    listening = true
    viewport.addEventListener('resize', update)
    viewport.addEventListener('scroll', update)
    update()
  }
  const stop = () => {
    inset.value = 0
    if (!viewport || !listening) return
    listening = false
    viewport.removeEventListener('resize', update)
    viewport.removeEventListener('scroll', update)
  }

  watch(() => toValue(active), (on) => (on ? start() : stop()), { immediate: true })
  onScopeDispose(stop)

  return { inset }
}
