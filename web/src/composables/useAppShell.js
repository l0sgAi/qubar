import { ref, computed, getCurrentInstance } from 'vue'
import { useRoute } from 'vue-router'
import { useBreakpoint } from '@/composables/useBreakpoint'
import { MOBILE_SHELL_ENABLED } from '@/constants/breakpoints'
import { getMobileMeta } from '@/router/mobileMeta'

// SideNav 展开 / 收起宽度（与 SideNav 的 :width / :collapsed-width 一致）
export const SIDENAV_WIDTH = 260
export const SIDENAV_COLLAPSED_WIDTH = 64

// 移动端内容区底部留白：有 TabBar 时让出 TabBar 高度，均计入 Home Indicator 安全区
const MOBILE_PADDING_WITH_TABBAR = 'calc(var(--tabbar-height) + var(--safe-bottom) + 16px)'
const MOBILE_PADDING_NO_TABBAR = 'calc(var(--safe-bottom) + 16px)'

/**
 * 当前是否使用移动端外框（总开关 && 移动端断点）。
 * AppShell 与页面各自调用，读的是同一个 useBreakpoint 单例，结果始终一致。
 */
export function useMobileShell() {
  const { isMobile } = useBreakpoint()
  return computed(() => MOBILE_SHELL_ENABLED && isMobile.value)
}

/**
 * 页面侧布局状态，配合 <AppShell v-model:offset="offset" /> 使用（见 docs/mobile-adaptation-plan.md 5.3）。
 * AppShell 只渲染外框，页面内容是它的兄弟节点：页面 scoped 样式照常命中，跨断点也不会 remount。
 *
 * @returns {{ offset: Ref<number>, mobileShell: ComputedRef<boolean>, contentStyle: ComputedRef<Object> }}
 *   offset       - 侧栏当前宽度，由 AppShell 内的 SideNav 展开/收起回写
 *   mobileShell  - 是否移动端外框（页面据此隐藏 RightSidebar 等桌面专属块）
 *   contentStyle - 内容容器的内联样式：桌面让出侧栏宽度；移动端无侧栏，底部让出 TabBar
 */
export function useAppShell() {
  const mobileShell = useMobileShell()
  // 组件外（单测直接调用）无路由上下文，按默认 meta 处理
  const route = getCurrentInstance() ? useRoute() : null
  const offset = ref(SIDENAV_WIDTH)
  const contentStyle = computed(() => {
    if (!mobileShell.value) {
      return { 'margin-left': `${offset.value}px`, width: `calc(100% - ${offset.value}px)` }
    }
    const { tabBar } = getMobileMeta(route)
    return { 'padding-bottom': tabBar ? MOBILE_PADDING_WITH_TABBAR : MOBILE_PADDING_NO_TABBAR }
  })
  return { offset, mobileShell, contentStyle }
}
