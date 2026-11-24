// 路由级移动端配置 meta.mobile（见 docs/mobile-adaptation-plan.md 5.4），缺省字段取默认值：
// - supported：移动端是否支持该页；false 时 App.vue 改渲染「暂不支持页」（P1），URL 不变
// - topBar：移动端顶栏形态 —— main（logo + 搜索）/ back（返回 + 标题 + 页面操作）
// - tabBar：是否显示底部 TabBar

export const MOBILE_TOP_BAR = ['main', 'back']

export const MOBILE_META_DEFAULTS = Object.freeze({
  supported: true,
  topBar: 'main',
  tabBar: true
})

/** 合并默认值，返回完整的移动端配置。 */
export function getMobileMeta(route) {
  return { ...MOBILE_META_DEFAULTS, ...(route?.meta?.mobile || {}) }
}

/** 移动端外框下该路由是否改渲染「暂不支持页」（App.vue 使用）。 */
export function isUnsupportedOnMobile(route, mobileShell) {
  return Boolean(mobileShell) && getMobileMeta(route).supported === false
}
