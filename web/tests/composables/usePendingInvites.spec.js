import { describe, it, expect, vi, beforeEach } from 'vitest'
import { effectScope } from 'vue'
import { auth } from '@/utils/auth'
import { getMyInvites } from '@/api/circleInvite'
import {
  usePendingInvites, refreshPendingInvites, beginPendingInvitesRead,
  setPendingInvites, __resetPendingInvites
} from '@/composables/usePendingInvites'

vi.mock('@/api/circleInvite', () => ({ getMyInvites: vi.fn() }))

const deferred = () => {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}

function deferCount() {
  const { promise, resolve } = deferred()
  getMyInvites.mockReturnValueOnce(promise)
  return (total) => resolve({ data: [], total })
}
const { pendingCount } = usePendingInvites()

beforeEach(() => {
  __resetPendingInvites()
  auth.setToken('session-a')
  getMyInvites.mockReset().mockResolvedValue({ data: [{}], total: 3 })
})

describe('usePendingInvites', () => {
  it('用 size=1 的顶层 total 计数，并合并同一会话的并发读', async () => {
    const counts = await Promise.all([refreshPendingInvites(), refreshPendingInvites()])
    expect(getMyInvites).toHaveBeenCalledWith({ page: 1, size: 1 })
    expect(getMyInvites).toHaveBeenCalledTimes(1)
    expect(counts).toEqual([3, 3])
    expect(pendingCount.value).toBe(3)
  })

  it('同会话 60 秒内复用缓存，过期或强制刷新时重新读取', async () => {
    vi.useFakeTimers()
    await refreshPendingInvites()
    vi.advanceTimersByTime(59999)
    await refreshPendingInvites()
    expect(getMyInvites).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(1)
    await refreshPendingInvites()
    getMyInvites.mockResolvedValueOnce({ total: 0 })
    await refreshPendingInvites({ force: true })
    expect(getMyInvites).toHaveBeenCalledTimes(3)
    expect(pendingCount.value).toBe(0)
  })

  it('请求失败保留本账号上次成功的总数，再次进入时重试', async () => {
    setPendingInvites(2, beginPendingInvitesRead())
    getMyInvites.mockRejectedValueOnce(new Error('Network Error'))
    await refreshPendingInvites({ force: true })
    expect(pendingCount.value).toBe(2)
    await refreshPendingInvites()
    expect(pendingCount.value).toBe(3)
  })

  it('60 秒内切账号清空前一人的角标，挂载的入口自动重新取数', async () => {
    const scope = effectScope()
    try {
      scope.run(usePendingInvites)
      await refreshPendingInvites()
      const finishB = deferCount()
      auth.setToken('session-b')
      expect(pendingCount.value).toBe(0)
      const readB = refreshPendingInvites()
      finishB(1)
      await readB
      expect(pendingCount.value).toBe(1)
      expect(getMyInvites).toHaveBeenCalledTimes(2)
    } finally {
      scope.stop()
    }
  })

  it('A 退出后 B 强刷返回 1，A 迟到的 9 不污染 B，也不接管 B 的缓存', async () => {
    const finishA = deferCount()
    const readA = refreshPendingInvites()
    auth.clearToken()
    expect(await refreshPendingInvites()).toBe(0)
    auth.setToken('session-b')
    getMyInvites.mockResolvedValueOnce({ total: 1 })
    await refreshPendingInvites({ force: true })
    finishA(9)
    await readA
    expect(await refreshPendingInvites()).toBe(1)
    expect(getMyInvites).toHaveBeenCalledTimes(2)
  })

  it('退出后 A 的在途角标、列表返回均无法回写匿名态', async () => {
    const version = beginPendingInvitesRead()
    const finish = deferCount()
    const read = refreshPendingInvites()
    auth.clearToken()
    finish(9)
    await read
    setPendingInvites(8, version)
    expect(await refreshPendingInvites()).toBe(0)
    expect(pendingCount.value).toBe(0)
    expect(getMyInvites).toHaveBeenCalledTimes(1)
  })

  it('同账号 force 乱序：旧响应无法覆盖新值，也不能清除新的在途合并', async () => {
    const finish1 = deferCount()
    const first = refreshPendingInvites({ force: true })
    const finish2 = deferCount()
    const second = refreshPendingInvites({ force: true })
    const finish3 = deferCount()
    const third = refreshPendingInvites({ force: true })
    finish2(8)
    await second
    const joined = refreshPendingInvites()
    expect(getMyInvites).toHaveBeenCalledTimes(3)
    finish3(1)
    await Promise.all([third, joined])
    finish1(9)
    await first
    expect(pendingCount.value).toBe(1)
  })

  it('处理邀请后的新列表写入 0，旧角标和旧列表不能将它重新点亮', async () => {
    const finish = deferCount()
    const read = refreshPendingInvites()
    const oldList = beginPendingInvitesRead()
    setPendingInvites(0, beginPendingInvitesRead())
    finish(9)
    await read
    setPendingInvites(8, oldList)
    expect(await refreshPendingInvites()).toBe(0)
    expect(getMyInvites).toHaveBeenCalledTimes(1)
  })
})
