/**
 * 访客可读接口白名单（对接后端 anonymous 开放改造）。
 *
 * 语义见 docs/guest-access-api.md：
 * - 列表中的接口「未登录也能正常调用」，不会返回 401。
 * - 部分接口在匿名访问时，个性化字段（is_liked/is_collected/is_joined）会降级为 false，
 *   这是后端预期行为，前端无需特殊处理。
 *
 * isGuestAccessible 用于接口契约自检，响应拦截器使用 isFeedTabRestricted 单独处理受限 feed。
 */

/**
 * 访客可读接口（按 METHOD + URL 前缀匹配）。
 * URL 取 pathname（去掉 query），前缀匹配以支持 /circle/detail/:id 这类带 id 的路径。
 *
 * /post/home 单独特判：仅 tab=hot|latest 访客可读，recommend|following 不可读。
 */
const GUEST_ACCESSIBLE = [
  // —— 纯只读（B 级）——
  { method: 'GET', path: '/category/get' },
  { method: 'GET', path: '/user/search' },
  { method: 'GET', path: '/user/detail' }, // /user/detail/:id
  { method: 'GET', path: '/circle/list' },
  { method: 'GET', path: '/circle/active' },
  { method: 'GET', path: '/circle/random' },
  { method: 'GET', path: '/circle/invite/preview' },
  { method: 'GET', path: '/circle/user' },
  { method: 'GET', path: '/circle/posts' },
  { method: 'GET', path: '/circle/detail' }, // /circle/detail/:id
  { method: 'GET', path: '/post/list' },
  { method: 'GET', path: '/post/user' }, // /post/user/:user_id
  { method: 'GET', path: '/post/detail' }, // /post/detail/:id
  { method: 'GET', path: '/comment/list' },
  { method: 'GET', path: '/comment/replies' },
  { method: 'GET', path: '/comment/locate' },
  { method: 'GET', path: '/comment/detail' }, // /comment/detail/:id
  { method: 'GET', path: '/trending' },
  { method: 'GET', path: '/discover' },
  // —— 用户关注（后端 v0.9.0 #217 / #218）：计数与列表访客可读，关注 / 互关 / 批量关系需登录 ——
  { method: 'GET', path: '/follow/stats' }, // /follow/stats/:user_id
  { method: 'GET', path: '/follow/following' }, // /follow/following/:user_id
  { method: 'GET', path: '/follow/followers' }, // /follow/followers/:user_id
  // /post/home 不在此处直接列出——由 isGuestAccessible 内部按 tab 特判
]

/**
 * 规范化 URL：去掉 query/hash，返回 pathname。
 * 支持完整 URL（含 baseURL）和纯 pathname 两种输入。
 */
function normalizePath(url) {
  if (!url) return ''
  // 去掉 protocol + host（如有）
  let path = url
  try {
    if (/^https?:\/\//.test(path)) {
      path = new URL(path).pathname
    }
  } catch {
    // 不是完整 URL，按字符串处理
  }
  // 去掉 query / hash
  path = path.split('?')[0].split('#')[0]
  // 去掉尾部斜杠（保留根路径）
  if (path.length > 1 && path.endsWith('/')) path = path.slice(0, -1)
  return path
}

/**
 * 判断一个请求是否「访客可读」。
 * @param {string} method HTTP 方法（大小写不敏感）
 * @param {string} url 原始 URL（可含 query/baseURL）
 * @returns {boolean}
 */
export function isGuestAccessible(method, url) {
  if (!method || !url) return false
  const m = String(method).toUpperCase()
  const path = normalizePath(url)

  // /post/home 按 tab 特判
  if (m === 'GET' && path === '/post/home') {
    const tab = new URLSearchParams(url.split('#')[0].split('?')[1] || '').get('tab')
    // 未指定 tab 或 tab 为空时，请求无效（400）。
    return tab === 'hot' || tab === 'latest'
  }

  return GUEST_ACCESSIBLE.some(
    (rule) => rule.method === m && (path === rule.path || path.startsWith(rule.path + '/'))
  )
}

/**
 * 后端约定的「该 feed tab 需要登录」错误 message。
 * 仅 /post/home?tab=recommend|following 访客访问时返回，与其他 401（token 过期）语义不同。
 */
const FEED_TAB_RESTRICTED_MESSAGE = 'This feed tab requires login'

/**
 * 判断 401 是否为「feed tab 访问限制」——这类 401 不应清 token 或跳登录，
 * 应交由调用方降级处理（如切到 hot tab 或弹登录引导）。
 */
export function isFeedTabRestricted(message) {
  return message === FEED_TAB_RESTRICTED_MESSAGE
}

export { GUEST_ACCESSIBLE }
