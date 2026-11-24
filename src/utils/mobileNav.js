// 移动端导航纯函数（见 docs/mobile-adaptation-plan.md 5.4 / 6.2），便于单测

/** 站内落地页：登录用户回主页，访客回发现页（避免被 /home 的登录守卫弹走）。 */
export const homePath = (loggedIn) => (loggedIn ? '/home' : '/discover')

/**
 * 顶栏返回：站内有上一页时后退；深链直开（无站内历史）时回落地页，避免退出站点。
 * vue-router 4 在 history.state.back 记录站内上一页路径，外链进入时为 null。
 */
export const goBack = (router, loggedIn, historyState = window.history.state) => {
  if (historyState?.back) {
    router.back()
  } else {
    router.replace(homePath(loggedIn))
  }
}

/** 登录页地址：带 redirect，登录成功后回到当前页（与 LoginPromptModal 约定一致）。 */
export const loginLocation = (currentFullPath) => ({
  path: '/',
  query: {
    redirect: currentFullPath && currentFullPath !== '/' ? currentFullPath : '/home',
    tab: 'login'
  }
})

// TabBar 各项的高亮规则：按路径前缀匹配
const TAB_MATCH = {
  home: (path) => path === '/home',
  discover: (path) => path === '/discover' || path === '/hot' || path === '/search',
  // 私信：会话列表与聊天页（聊天页移动端不显示 TabBar，这里只为语义完整）
  messages: (path) => path === '/messages' || path.startsWith('/messages/'),
  notice: (path) => path === '/notifications',
  me: (path) => path === '/profile' || path === '/invites'
}

/** 当前路径对应的 TabBar 项（无匹配返回 null，如帖子详情、圈子页）。 */
export const activeTab = (path) => Object.keys(TAB_MATCH).find(key => TAB_MATCH[key](path)) || null
