import { describe, it, expect, vi, beforeEach } from 'vitest'
import { effectScope, nextTick, ref } from 'vue'

const api = vi.hoisted(() => ({ followUser: vi.fn(), unfollowUser: vi.fn() }))
vi.mock('@/api/follow', () => ({ followUser: api.followUser, unfollowUser: api.unfollowUser }))

const guest = vi.hoisted(() => ({ requireLogin: vi.fn() }))
vi.mock('@/utils/guest-action', () => ({ requireLogin: guest.requireLogin }))

const { useFollowToggle, onFollowRelationChange } = await import('@/composables/useFollowToggle')

const login = () => {
  localStorage.setItem('qubar_token', 'tok')
  localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
}
const logout = () => {
  localStorage.removeItem('qubar_token')
  localStorage.removeItem('qubar_token_expire')
}

const deferred = () => {
  let resolve, reject
  const promise = new Promise((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

// 在 effectScope 里创建，测试结束时 stop 以清理订阅
const setup = (opts) => {
  const scope = effectScope()
  const result = scope.run(() => useFollowToggle(opts))
  return { ...result, stop: () => scope.stop() }
}

describe('useFollowToggle', () => {
  beforeEach(() => {
    api.followUser.mockReset()
    api.unfollowUser.mockReset()
    guest.requireLogin.mockReset()
    login()
  })

  it('访客点击弹登录引导，不发请求', async () => {
    logout()
    const { toggle, relation, stop } = setup({ userId: () => 'u1' })
    await toggle()
    expect(guest.requireLogin).toHaveBeenCalledWith('follow')
    expect(api.followUser).not.toHaveBeenCalled()
    expect(relation.value).toBe(0)
    stop()
  })

  it('0 → 关注，用响应 relation 更新并回调 onChange', async () => {
    api.followUser.mockResolvedValue({ data: { relation: 1 } })
    const onChange = vi.fn()
    const { toggle, relation, stop } = setup({ userId: () => 'u1', relation: () => 0, onChange })
    await toggle()
    expect(api.followUser).toHaveBeenCalledWith('u1')
    expect(relation.value).toBe(1)
    expect(onChange).toHaveBeenCalledWith({ relation: 1, prev: 0 })
    stop()
  })

  it('2（回关）→ 关注后变互关 3；3 → 取关后变 2', async () => {
    api.followUser.mockResolvedValue({ data: { relation: 3 } })
    api.unfollowUser.mockResolvedValue({ data: { relation: 2 } })
    const { toggle, relation, stop } = setup({ userId: () => 'u1', relation: () => 2 })
    await toggle()
    expect(relation.value).toBe(3)
    await toggle()
    expect(api.unfollowUser).toHaveBeenCalledWith('u1')
    expect(relation.value).toBe(2)
    stop()
  })

  it('1 → 取关变 0', async () => {
    api.unfollowUser.mockResolvedValue({ data: { relation: 0 } })
    const { toggle, relation, stop } = setup({ userId: () => 'u1', relation: () => 1 })
    await toggle()
    expect(relation.value).toBe(0)
    stop()
  })

  it('请求进行中重复点击只发一次', async () => {
    const d = deferred()
    api.followUser.mockReturnValue(d.promise)
    const { toggle, pending, relation, stop } = setup({ userId: () => 'u1' })
    const first = toggle()
    expect(pending.value).toBe(true)
    await toggle()
    await toggle()
    expect(api.followUser).toHaveBeenCalledTimes(1)
    d.resolve({ data: { relation: 1 } })
    await first
    expect(pending.value).toBe(false)
    expect(relation.value).toBe(1)
    stop()
  })

  it('失败时关系不变，并回调错误文案 key', async () => {
    api.followUser.mockRejectedValue(Object.assign(new Error('following limit reached: you can follow at most 2000 users'), { code: 201, status: 400 }))
    const onError = vi.fn()
    const { toggle, relation, pending, stop } = setup({ userId: () => 'u1', onError })
    await toggle()
    expect(relation.value).toBe(0)
    expect(pending.value).toBe(false)
    expect(onError).toHaveBeenCalledWith('follow.error.limitReached')
    stop()
  })

  it('没有 userId 时不发请求', async () => {
    const { toggle, stop } = setup({ userId: () => '' })
    await toggle()
    expect(api.followUser).not.toHaveBeenCalled()
    stop()
  })

  it('外部 relation 变化时同步（进行中的请求优先）；非法值按 0', async () => {
    const outer = ref(1)
    const { relation, stop } = setup({ userId: () => 'u1', relation: () => outer.value })
    expect(relation.value).toBe(1)
    outer.value = 3
    await nextTick()
    expect(relation.value).toBe(3)
    outer.value = 42
    await nextTick()
    expect(relation.value).toBe(0)
    stop()
  })

  it('同一用户的其他实例操作成功后同步，其他用户不受影响；scope 销毁后不再同步', async () => {
    api.followUser.mockResolvedValue({ data: { relation: 3 } })
    const a = setup({ userId: () => 'u1', relation: () => 2 })
    const b = setup({ userId: () => 'u1', relation: () => 2 })
    const c = setup({ userId: () => 'u2', relation: () => 0 })
    const seen = vi.fn()
    const off = onFollowRelationChange(seen)
    await a.toggle()
    expect(b.relation.value).toBe(3)
    expect(c.relation.value).toBe(0)
    expect(seen).toHaveBeenCalledWith({ userId: 'u1', relation: 3, prev: 2 })
    off()

    b.stop()
    api.unfollowUser.mockResolvedValue({ data: { relation: 2 } })
    await a.toggle()
    expect(b.relation.value).toBe(3)
    a.stop()
    c.stop()
  })

  it('某个订阅者抛错不影响其他订阅者', async () => {
    api.followUser.mockResolvedValue({ data: { relation: 1 } })
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const offBad = onFollowRelationChange(() => { throw new Error('boom') })
    const good = vi.fn()
    const offGood = onFollowRelationChange(good)
    const { toggle, stop } = setup({ userId: () => 'u1' })
    await toggle()
    expect(good).toHaveBeenCalled()
    offBad()
    offGood()
    stop()
  })

  it('不在 effect scope 内也能使用（不注册清理）', async () => {
    api.followUser.mockResolvedValue({ data: {} })
    const { toggle, relation } = useFollowToggle({ userId: () => 'u9' })
    await toggle()
    expect(relation.value).toBe(0)
  })
})
