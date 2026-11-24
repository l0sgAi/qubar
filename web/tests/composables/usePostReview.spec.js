import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { effectScope } from 'vue'
import { useReviewPolling, isPostPublished, POST_STATUS } from '@/composables/usePostReview'

const INTERVAL = 3000
const MAX = 10
const tick = (ms) => vi.advanceTimersByTimeAsync(ms)

// 按顺序返回给定状态序列，序列用完后一直返回最后一个
const statusSequence = (...statuses) => {
  let i = 0
  return vi.fn(async () => statuses[Math.min(i++, statuses.length - 1)])
}

describe('useReviewPolling', () => {
  let scope

  const setup = (fetchStatus, onSettled = vi.fn()) => {
    scope = effectScope()
    const poll = scope.run(() => useReviewPolling({ fetchStatus, onSettled, interval: INTERVAL, maxAttempts: MAX }))
    return { poll, onSettled }
  }

  beforeEach(() => {
    vi.useFakeTimers()
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    scope?.stop()
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('每 3 秒拉一次，状态仍为 2 时继续', async () => {
    const fetchStatus = statusSequence(POST_STATUS.REVIEWING)
    const { poll } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL - 1)
    expect(fetchStatus).not.toHaveBeenCalled()
    await tick(1)
    expect(fetchStatus).toHaveBeenCalledTimes(1)
    await tick(INTERVAL)
    expect(fetchStatus).toHaveBeenCalledTimes(2)
    expect(poll.polling.value).toBe(true)
  })

  it('10 次后仍为 2：停止轮询并置 exhausted', async () => {
    const fetchStatus = statusSequence(POST_STATUS.REVIEWING)
    const { poll, onSettled } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL * MAX)
    expect(fetchStatus).toHaveBeenCalledTimes(MAX)
    expect(poll.exhausted.value).toBe(true)
    expect(poll.polling.value).toBe(false)
    await tick(INTERVAL * 5)
    expect(fetchStatus).toHaveBeenCalledTimes(MAX)
    expect(onSettled).not.toHaveBeenCalled()
  })

  it('变为 1：立即停止并回调 onSettled(1)', async () => {
    const fetchStatus = statusSequence(POST_STATUS.REVIEWING, POST_STATUS.PUBLISHED)
    const { poll, onSettled } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL * 2)
    expect(onSettled).toHaveBeenCalledWith(POST_STATUS.PUBLISHED)
    expect(poll.polling.value).toBe(false)
    expect(poll.exhausted.value).toBe(false)
    await tick(INTERVAL * 5)
    expect(fetchStatus).toHaveBeenCalledTimes(2)
  })

  it('变为 3：停止并回调 onSettled(3)', async () => {
    const { poll, onSettled } = setup(statusSequence(POST_STATUS.REJECTED))
    poll.start()
    await tick(INTERVAL)
    expect(onSettled).toHaveBeenCalledWith(POST_STATUS.REJECTED)
    expect(poll.polling.value).toBe(false)
  })

  it('fetchStatus 返回 null（帖子已不可见）：停止且不回调', async () => {
    const fetchStatus = statusSequence(null)
    const { poll, onSettled } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL * 3)
    expect(fetchStatus).toHaveBeenCalledTimes(1)
    expect(poll.polling.value).toBe(false)
    expect(onSettled).not.toHaveBeenCalled()
  })

  it('请求失败计入次数并继续，10 次后停止', async () => {
    const fetchStatus = vi.fn().mockRejectedValue(new Error('timeout'))
    const { poll } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL * (MAX + 3))
    expect(fetchStatus).toHaveBeenCalledTimes(MAX)
    expect(poll.exhausted.value).toBe(true)
  })

  it('stop 后不再请求，在途请求的结果被丢弃', async () => {
    let resolve
    const fetchStatus = vi.fn(() => new Promise((r) => { resolve = r }))
    const { poll, onSettled } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL)
    expect(fetchStatus).toHaveBeenCalledTimes(1)
    poll.stop()
    resolve(POST_STATUS.PUBLISHED)
    await tick(INTERVAL * 5)
    expect(onSettled).not.toHaveBeenCalled()
    expect(fetchStatus).toHaveBeenCalledTimes(1)
  })

  it('所在 scope 销毁（离开页面）时清掉定时器', async () => {
    const fetchStatus = statusSequence(POST_STATUS.REVIEWING)
    const { poll } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL)
    scope.stop()
    await tick(INTERVAL * 5)
    expect(fetchStatus).toHaveBeenCalledTimes(1)
    expect(poll.polling.value).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('重新 start（切到另一个审核中帖子）时次数归零', async () => {
    const fetchStatus = statusSequence(POST_STATUS.REVIEWING)
    const { poll } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL * MAX)
    expect(poll.exhausted.value).toBe(true)
    poll.start()
    expect(poll.exhausted.value).toBe(false)
    expect(poll.attempts.value).toBe(0)
    await tick(INTERVAL)
    expect(fetchStatus).toHaveBeenCalledTimes(MAX + 1)
  })

  it('手动刷新：立即拉取、不计入次数，并从现在起重新计时', async () => {
    const fetchStatus = statusSequence(POST_STATUS.REVIEWING)
    const { poll } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL - 1000)
    await poll.refresh()
    expect(fetchStatus).toHaveBeenCalledTimes(1)
    expect(poll.attempts.value).toBe(0)
    await tick(INTERVAL - 1)
    expect(fetchStatus).toHaveBeenCalledTimes(1)
    await tick(1)
    expect(fetchStatus).toHaveBeenCalledTimes(2)
    expect(poll.attempts.value).toBe(1)
  })

  it('手动刷新失败：轮询中则继续计时，refreshing 复位', async () => {
    const fetchStatus = statusSequence(POST_STATUS.REVIEWING)
    const { poll } = setup(fetchStatus)
    poll.start()
    fetchStatus.mockRejectedValueOnce(new Error('timeout'))
    await poll.refresh()
    expect(poll.refreshing.value).toBe(false)
    expect(poll.polling.value).toBe(true)
    await tick(INTERVAL)
    expect(fetchStatus).toHaveBeenCalledTimes(2)
  })

  it('轮询已停止（exhausted）后手动刷新拿到 1：回调 onSettled 且不重启轮询', async () => {
    const fetchStatus = statusSequence(POST_STATUS.REVIEWING)
    const { poll, onSettled } = setup(fetchStatus)
    poll.start()
    await tick(INTERVAL * MAX)
    fetchStatus.mockResolvedValueOnce(POST_STATUS.REVIEWING)
    await poll.refresh()
    expect(poll.polling.value).toBe(false)
    expect(vi.getTimerCount()).toBe(0)
    fetchStatus.mockResolvedValueOnce(POST_STATUS.PUBLISHED)
    await poll.refresh()
    expect(onSettled).toHaveBeenCalledWith(POST_STATUS.PUBLISHED)
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('isPostPublished', () => {
  it('只有 status=1 视为已发布', () => {
    expect(isPostPublished({ status: 1 })).toBe(true)
    expect(isPostPublished({ status: 2 })).toBe(false)
    expect(isPostPublished({ status: 3 })).toBe(false)
    expect(isPostPublished(null)).toBe(false)
  })
})
