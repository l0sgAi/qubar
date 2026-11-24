// 关注 / 粉丝 / 互关列表的游标分页状态（后端 v0.9.0 GET /follow/following|followers/:id、/follow/mutual）。
//
// - 游标不透明，原样回传；列表是否结束**只看 has_more**（服务端会过滤禁用 / 删除用户，一页可能不足 size 甚至为空）
// - 空页但 has_more=true 时自动续拉下一页（有上限，避免异常数据导致死循环）
// - reset() 换列表（如 关注 ↔ 粉丝）后，旧请求的响应直接丢弃
// - 403（对方列表私密）/ 404（用户不存在）单独标记，页面按状态渲染
import { ref } from 'vue'
import { FOLLOW_PAGE_SIZE, isFollowListPrivateError, isFollowUserNotFoundError } from '@/utils/follow'

export const FOLLOW_LIST_ERROR = Object.freeze({
  PRIVATE: 'private',
  NOT_FOUND: 'notFound',
  GENERIC: 'generic'
})

// 连续空页自动续拉的上限
const MAX_EMPTY_PAGE_CHAIN = 5

/**
 * @param {Object} options
 * @param {(params: { cursor?: string, size: number }) => Promise} options.fetchPage - 返回响应信封，data: { items, has_more, next_cursor }
 * @param {number} [options.size]
 */
export function useFollowList({ fetchPage, size = FOLLOW_PAGE_SIZE } = {}) {
  const items = ref([])
  const hasMore = ref(true)
  const loading = ref(false)
  const loaded = ref(false) // 至少成功加载过一页
  const error = ref(null) // FOLLOW_LIST_ERROR.*，null 表示无错误
  let cursor = ''
  let gen = 0

  const classify = (err) => {
    if (isFollowListPrivateError(err)) return FOLLOW_LIST_ERROR.PRIVATE
    if (isFollowUserNotFoundError(err)) return FOLLOW_LIST_ERROR.NOT_FOUND
    return FOLLOW_LIST_ERROR.GENERIC
  }

  const append = (incoming) => {
    const seen = new Set(items.value.map(it => it?.user?.id))
    const fresh = []
    for (const it of incoming) {
      const id = it?.user?.id
      if (!id || seen.has(id)) continue
      seen.add(id)
      fresh.push(it)
    }
    if (fresh.length) items.value = [...items.value, ...fresh]
  }

  /** 加载下一页；返回是否成功 */
  const loadMore = async () => {
    if (loading.value || !hasMore.value) return false
    const myGen = gen
    loading.value = true
    error.value = null
    try {
      let chain = 0
      // 空页且还有下一页：继续拉，直到拿到数据 / 结束 / 达到上限
      for (;;) {
        const params = { size }
        if (cursor) params.cursor = cursor
        const res = await fetchPage(params)
        if (myGen !== gen) return false
        const data = res?.data || {}
        const page = Array.isArray(data.items) ? data.items : []
        append(page)
        hasMore.value = !!data.has_more
        cursor = hasMore.value ? (data.next_cursor || '') : ''
        // has_more=true 却没有游标：无法继续，视为结束，避免用空游标重拉第一页
        if (hasMore.value && !cursor) hasMore.value = false
        loaded.value = true
        if (page.length > 0 || !hasMore.value || ++chain >= MAX_EMPTY_PAGE_CHAIN) break
      }
      return true
    } catch (err) {
      if (myGen !== gen) return false
      error.value = classify(err)
      // 私密 / 不存在：不会因重试而改变，停止翻页
      if (error.value !== FOLLOW_LIST_ERROR.GENERIC) hasMore.value = false
      return false
    } finally {
      if (myGen === gen) loading.value = false
    }
  }

  /** 清空并从第一页重新加载（可换 fetchPage） */
  const reset = (nextFetchPage) => {
    if (nextFetchPage) fetchPage = nextFetchPage
    gen++
    items.value = []
    hasMore.value = true
    loading.value = false
    loaded.value = false
    error.value = null
    cursor = ''
    return loadMore()
  }

  /** 更新某个用户的 relation（列表项按钮操作后同步） */
  const setRelation = (userId, relation) => {
    const it = items.value.find(x => x?.user?.id === userId)
    if (it) it.relation = relation
  }

  return { items, hasMore, loading, loaded, error, loadMore, reset, setRelation }
}
