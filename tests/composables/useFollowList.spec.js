import { describe, it, expect, vi } from 'vitest'
import { useFollowList, FOLLOW_LIST_ERROR } from '@/composables/useFollowList'

const item = (id, relation = 0) => ({ user: { id, username: id, avatar_url: '' }, follow_time: '', relation })
const page = (ids, hasMore, next = '') => ({ data: { items: ids.map(id => item(id)), has_more: hasMore, next_cursor: next } })
const apiError = (status, code) => Object.assign(new Error('x'), { status, code })

const deferred = () => {
  let resolve
  const promise = new Promise((res) => { resolve = res })
  return { promise, resolve }
}

describe('useFollowList', () => {
  it('游标原样回传，以 has_more 结束', async () => {
    const fetchPage = vi.fn()
      .mockResolvedValueOnce(page(['a', 'b'], true, 'c1'))
      .mockResolvedValueOnce(page(['c'], false, ''))
    const list = useFollowList({ fetchPage, size: 2 })
    await list.loadMore()
    expect(fetchPage).toHaveBeenLastCalledWith({ size: 2 })
    expect(list.items.value.map(i => i.user.id)).toEqual(['a', 'b'])
    expect(list.hasMore.value).toBe(true)
    await list.loadMore()
    expect(fetchPage).toHaveBeenLastCalledWith({ size: 2, cursor: 'c1' })
    expect(list.items.value.map(i => i.user.id)).toEqual(['a', 'b', 'c'])
    expect(list.hasMore.value).toBe(false)
    expect(list.loaded.value).toBe(true)
    // 结束后不再请求
    await list.loadMore()
    expect(fetchPage).toHaveBeenCalledTimes(2)
  })

  it('不足 size 条但 has_more=true 时继续翻页（不以条数判断结束）', async () => {
    const fetchPage = vi.fn()
      .mockResolvedValueOnce(page(['a'], true, 'c1'))
      .mockResolvedValueOnce(page(['b'], false))
    const list = useFollowList({ fetchPage, size: 20 })
    await list.loadMore()
    expect(list.hasMore.value).toBe(true)
    await list.loadMore()
    expect(list.items.value).toHaveLength(2)
  })

  it('空页且 has_more=true 时自动续拉', async () => {
    const fetchPage = vi.fn()
      .mockResolvedValueOnce(page([], true, 'c1'))
      .mockResolvedValueOnce(page([], true, 'c2'))
      .mockResolvedValueOnce(page(['a'], true, 'c3'))
    const list = useFollowList({ fetchPage })
    await list.loadMore()
    expect(fetchPage).toHaveBeenCalledTimes(3)
    expect(fetchPage).toHaveBeenLastCalledWith({ size: 20, cursor: 'c2' })
    expect(list.items.value).toHaveLength(1)
    expect(list.hasMore.value).toBe(true)
  })

  it('连续空页达到上限后停下，等下一次触发', async () => {
    const fetchPage = vi.fn().mockImplementation(async () => page([], true, 'cx'))
    const list = useFollowList({ fetchPage })
    await list.loadMore()
    expect(fetchPage).toHaveBeenCalledTimes(5)
    expect(list.hasMore.value).toBe(true)
  })

  it('has_more=true 却没有游标：视为结束，不用空游标重拉第一页', async () => {
    const fetchPage = vi.fn().mockResolvedValue(page(['a'], true, ''))
    const list = useFollowList({ fetchPage })
    await list.loadMore()
    expect(list.hasMore.value).toBe(false)
  })

  it('翻页中出现重复用户时去重；缺 data / items 时按空页处理', async () => {
    const fetchPage = vi.fn()
      .mockResolvedValueOnce(page(['a', 'b'], true, 'c1'))
      .mockResolvedValueOnce({ data: { items: [item('b'), item('c'), { user: null }], has_more: true, next_cursor: 'c2' } })
      .mockResolvedValueOnce({})
    const list = useFollowList({ fetchPage })
    await list.loadMore()
    await list.loadMore()
    expect(list.items.value.map(i => i.user.id)).toEqual(['a', 'b', 'c'])
    await list.loadMore()
    expect(list.hasMore.value).toBe(false)
  })

  it('403 标记为私密并停止翻页；404 标记为用户不存在', async () => {
    const priv = useFollowList({ fetchPage: vi.fn().mockRejectedValue(apiError(403, 203)) })
    expect(await priv.loadMore()).toBe(false)
    expect(priv.error.value).toBe(FOLLOW_LIST_ERROR.PRIVATE)
    expect(priv.hasMore.value).toBe(false)
    expect(priv.loading.value).toBe(false)

    const missing = useFollowList({ fetchPage: vi.fn().mockRejectedValue(apiError(404, 204)) })
    await missing.loadMore()
    expect(missing.error.value).toBe(FOLLOW_LIST_ERROR.NOT_FOUND)
  })

  it('其它错误可重试：保留 has_more，重试成功后清除错误', async () => {
    const fetchPage = vi.fn()
      .mockRejectedValueOnce(apiError(500, 500))
      .mockResolvedValueOnce(page(['a'], false))
    const list = useFollowList({ fetchPage })
    await list.loadMore()
    expect(list.error.value).toBe(FOLLOW_LIST_ERROR.GENERIC)
    expect(list.hasMore.value).toBe(true)
    await list.loadMore()
    expect(list.error.value).toBeNull()
    expect(list.items.value).toHaveLength(1)
  })

  it('加载中再次调用直接返回', async () => {
    const d = deferred()
    const fetchPage = vi.fn().mockReturnValue(d.promise)
    const list = useFollowList({ fetchPage })
    const first = list.loadMore()
    expect(await list.loadMore()).toBe(false)
    d.resolve(page(['a'], false))
    expect(await first).toBe(true)
    expect(fetchPage).toHaveBeenCalledTimes(1)
  })

  it('reset 换列表后丢弃旧请求的响应与错误', async () => {
    const old = deferred()
    const oldFail = deferred()
    const fetchOld = vi.fn().mockReturnValueOnce(old.promise)
    const list = useFollowList({ fetchPage: fetchOld })
    const p1 = list.loadMore()
    const fetchNew = vi.fn().mockResolvedValue(page(['n'], false))
    await list.reset(fetchNew)
    old.resolve(page(['stale'], true, 'x'))
    expect(await p1).toBe(false)
    expect(list.items.value.map(i => i.user.id)).toEqual(['n'])
    expect(list.hasMore.value).toBe(false)

    // 旧请求失败同样被丢弃
    const fetchFail = vi.fn().mockReturnValueOnce(oldFail.promise)
    const list2 = useFollowList({ fetchPage: fetchFail })
    const p2 = list2.loadMore()
    await list2.reset(vi.fn().mockResolvedValue(page(['m'], false)))
    oldFail.resolve(Promise.reject(apiError(500, 500)))
    expect(await p2).toBe(false)
    expect(list2.error.value).toBeNull()
  })

  it('reset 不传参数时沿用原 fetchPage 从第一页重拉', async () => {
    const fetchPage = vi.fn().mockResolvedValue(page(['a'], true, 'c1'))
    const list = useFollowList({ fetchPage })
    await list.loadMore()
    await list.reset()
    expect(fetchPage).toHaveBeenLastCalledWith({ size: 20 })
    expect(list.items.value).toHaveLength(1)
  })

  it('setRelation 更新对应列表项', async () => {
    const list = useFollowList({ fetchPage: vi.fn().mockResolvedValue(page(['a', 'b'], false)) })
    await list.loadMore()
    list.setRelation('b', 3)
    list.setRelation('zzz', 1)
    expect(list.items.value.map(i => i.relation)).toEqual([0, 3])
  })
})
