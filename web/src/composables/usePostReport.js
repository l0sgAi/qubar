import { reactive } from 'vue'
import { auth } from '@/utils/auth'
import { getUserInfo } from '@/api/auth'
import { POST_STATUS } from '@/composables/usePostReview'

/**
 * 举报帖子的会话内共享状态（详情页头部、信息流卡片的「…」菜单共用）。
 *
 * - 已举报集合：后端不返回「当前用户是否已举报」，只能在举报成功或收到 409 后本地记住，
 *   本次会话内把菜单项置灰为「已举报」。
 * - 当前用户 id：判断是不是自己的帖子（自己的帖子不显示举报），整个会话只请求一次 /user/get。
 *
 * 登出 / 换账号不刷新页面，两份状态都按 token 分开存，换 token 后自然失效。
 */

const reportedKeys = reactive(new Set())
// token → { id, loaded }
const viewers = reactive({})
const pending = new Map()

const keyOf = (postId) => `${auth.getToken()}\n${postId}`

/** 本次会话内是否已举报过该帖子 */
export function isPostReported(postId) {
  return reportedKeys.has(keyOf(postId))
}

/** 记为已举报（举报成功或 409 重复举报） */
export function markPostReported(postId) {
  reportedKeys.add(keyOf(postId))
}

/**
 * 加载当前登录用户 id（同一 token 只请求一次，并发调用共用同一个请求）。
 * 请求失败时记为已加载、id 为空：菜单照常显示，举报自己的帖子由后端 400 兜底。
 * @returns {Promise<string|null>}
 */
export function ensureViewer() {
  if (!auth.isAuthenticated()) return Promise.resolve(null)
  const token = auth.getToken()
  if (viewers[token]?.loaded) return Promise.resolve(viewers[token].id)
  if (!pending.has(token)) {
    viewers[token] = { id: null, loaded: false }
    pending.set(token, getUserInfo()
      .then(res => res?.data?.id || null)
      .catch(() => null)
      .then(id => {
        viewers[token] = { id, loaded: true }
        pending.delete(token)
        return id
      }))
  }
  return pending.get(token)
}

/**
 * 是否显示「举报」入口：帖子已发布，且不是当前用户的帖子。
 * 访客显示（点击后弹登录引导）；已登录但还没拿到当前用户 id 时先不显示，避免作者看到一闪而过的入口。
 * @param {{ status?: number, authorId?: string|null }} post
 */
export function canReportPost({ status, authorId }) {
  if (status !== POST_STATUS.PUBLISHED) return false
  if (!auth.isAuthenticated()) return true
  const viewer = viewers[auth.getToken()]
  if (!viewer?.loaded) return false
  return !viewer.id || !authorId || viewer.id !== authorId
}

/**
 * 举报失败 → post.report.* 文案 key。后端 message 是英文，一律不展示。
 * 三种 400（参数非法 / reason_type 越界 / 举报自己）共用 code=201，合并为 invalid。
 * @param {Error & { code?: number, status?: number }} error
 * @returns {'already'|'tooOften'|'unavailable'|'invalid'|'failed'}
 */
export function reportErrorKey(error) {
  if (error?.code === 207 || error?.status === 409) return 'already'
  if (error?.code === 208 || error?.status === 429) return 'tooOften'
  if (error?.code === 204 || error?.status === 404) return 'unavailable'
  if (error?.code === 201 || error?.status === 400) return 'invalid'
  return 'failed'
}

// 仅供测试：清空会话状态
export function __resetPostReport() {
  reportedKeys.clear()
  for (const k of Object.keys(viewers)) delete viewers[k]
  pending.clear()
}
