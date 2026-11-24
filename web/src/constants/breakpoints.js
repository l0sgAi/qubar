/**
 * 响应式断点（单一事实源，见 docs/mobile-adaptation-plan.md 5.1）。
 * CSS 媒体查询无法引用变量，main.css 与各组件 @media 须手写同一数值：
 *   ≤ 768px → 移动端门户；> 768px → 桌面布局
 */
export const BP_MOBILE_MAX = 768
export const MOBILE_QUERY = `(max-width: ${BP_MOBILE_MAX}px)`

// 移动端外框（MobileTopBar / MobileTabBar）总开关。
// P0 为 false（手机仍渲染桌面布局）；P1 门户完成后打开。如需紧急回退到桌面布局，改回 false 即可
export const MOBILE_SHELL_ENABLED = true
