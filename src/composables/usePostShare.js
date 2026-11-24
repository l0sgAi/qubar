import { computed, reactive, toValue } from 'vue'
import { auth } from '@/utils/auth'
import { sharePost } from '@/api/post'
import { isShareRetryableError, postShareUrl, shareErrorKey, shareOrCopy } from '@/utils/share'

/**
 * 帖子分享（后端 v0.9.0 POST /post/share，docs/backend-handoff/v0.9.0.md §3.4）。
 *
 * 会话内共享一层「分享状态覆盖」，让列表卡片与详情页看到同一份结果（与 usePostReport 同样按 token 隔离）：
 * - 分享数按帖子存：服务端返回的 share_count。分享只增不减，展示取「接口字段」与覆盖值的较大者，
 *   列表里的旧数据不会把刚分享后的数字压回去，后端缓存短暂偏低时也不会倒退。
 * - 是否已分享按 token + 帖子存：分享不可撤销，置 true 后不再变回。接口字段缺失（搜索 / 圈内列表等
 *   不返回 is_shared）按未分享处理。
 */

// postId → 服务端最近一次返回的 share_count
const counts = reactive({})
// `${token}\n${postId}` → true
const sharedKeys = reactive(new Set())
// 同一账号对同一帖子同时最多一个计数请求，连点共用
const inflight = new Map()

const keyOf = (postId, token = auth.getToken()) => `${token || ''}\n${postId}`

const wait = (ms) => new Promise(resolve => setTimeout(resolve, ms))

const toCount = (value) => {
  if (value === null || value === undefined || value === '') return null
  const n = Number(value)
  return Number.isFinite(n) && n >= 0 ? n : null
}

/**
 * 合并接口字段与会话内覆盖值。
 * @param {string} postId
 * @param {{ count?: number|null, shared?: boolean }} fields 接口返回的 share_count / is_shared（可缺省）
 * @returns {{ count: number|null, shared: boolean }} count 为 null 表示接口没给分享数且本地也没有
 */
export function resolvePostShare(postId, { count, shared } = {}) {
  const fromApi = toCount(count)
  const local = counts[postId]
  let merged = fromApi
  if (local !== undefined) merged = fromApi === null ? local : Math.max(fromApi, local)
  return {
    count: merged,
    shared: !!shared || sharedKeys.has(keyOf(postId))
  }
}

/**
 * 组件内读取合并后的分享状态（参数可为 ref / getter / 普通值）。
 * @returns {import('vue').ComputedRef<{ count: number|null, shared: boolean }>}
 */
export function usePostShareState(postId, count, shared) {
  return computed(() => resolvePostShare(toValue(postId), {
    count: toValue(count),
    shared: toValue(shared)
  }))
}

/** 记录一次成功的分享计数（POST /post/share 的响应） */
export function applyShareResult(postId, data, token = auth.getToken()) {
  const n = toCount(data?.share_count)
  if (n !== null) counts[postId] = Math.max(n, counts[postId] ?? 0)
  if (token) sharedKeys.add(keyOf(postId, token))
}

/**
 * 调 POST /post/share 计数。503（code 212，服务端已回滚）自动重试一次，重试是安全的。
 * 并发的第二次点击复用在途请求，first_share 只交给第一次调用方，+1 动效不会重复播放。
 * @returns {Promise<{ share_count: number, first_share: boolean }>}
 */
export function recordPostShare(postId, { retryDelay = 800 } = {}) {
  const token = auth.getToken()
  const key = keyOf(postId, token)
  const pending = inflight.get(key)
  if (pending) return pending.then(data => ({ ...data, first_share: false }))

  const request = (async () => {
    let res
    try {
      res = await sharePost(postId, token)
    } catch (error) {
      if (!isShareRetryableError(error)) throw error
      await wait(retryDelay)
      if (auth.getToken() !== token || !auth.isAuthenticated()) throw error
      res = await sharePost(postId, token)
    }
    const data = res?.data || {}
    applyShareResult(postId, data, token)
    return { share_count: data.share_count, first_share: !!data.first_share }
  })()
  inflight.set(key, request)
  request.then(() => inflight.delete(key), () => inflight.delete(key))
  return request
}

/**
 * 点击「分享」的完整流程：先唤起系统分享 / 复制链接（必须在任何 await 之前，保留用户手势），
 * 然后登录用户再计数。用户取消系统分享、复制失败都不计数；访客只分享链接，不调接口（会 401 清 token）。
 * 计数失败不影响分享本身：404 → 提示内容不可分享，重试后仍 503 → 提示可重试，其它静默。
 *
 * @param {{ postId: string, title?: string }} params
 * @returns {Promise<{
 *   result: 'shared'|'cancelled'|'copied'|'failed',
 *   counted: boolean,
 *   firstShare: boolean,
 *   errorKey: string|null
 * }>}
 */
export async function sharePostLink({ postId, title = '' }, options) {
  const token = auth.getToken()
  const result = await shareOrCopy({ title, url: postShareUrl(postId) })
  const outcome = { result, counted: false, firstShare: false, errorKey: null }
  if (result !== 'shared' && result !== 'copied') return outcome
  if (auth.getToken() !== token || !auth.isAuthenticated()) return outcome
  try {
    const data = await recordPostShare(postId, options)
    outcome.counted = true
    outcome.firstShare = auth.getToken() === token && data.first_share
  } catch (error) {
    if (auth.getToken() === token) outcome.errorKey = shareErrorKey(error)
  }
  return outcome
}

// 仅供测试：清空会话状态
export function __resetPostShare() {
  for (const k of Object.keys(counts)) delete counts[k]
  sharedKeys.clear()
  inflight.clear()
}
