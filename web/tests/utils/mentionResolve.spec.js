import { describe, it, expect, vi, beforeEach } from 'vitest'

// 模块级缓存：每个用例重新导入一份干净的模块
let mod
beforeEach(async () => {
  vi.resetModules()
  mod = await import('@/utils/mentionResolve')
})

describe('mentionResolve', () => {
  it('MAX_MENTIONS 为 10', () => {
    expect(mod.MAX_MENTIONS).toBe(10)
  })

  it('seedUsers 建立大小写不敏感的 username → id 映射', () => {
    mod.seedUsers([{ id: 'u1', username: 'Alice' }])
    expect(mod.peekUserId('alice')).toBe('u1')
    expect(mod.peekUserId('ALICE')).toBe('u1')
  })

  it('knownUsernames 保留原样大小写', () => {
    mod.seedUsers([{ id: 'u1', username: 'John Doe' }, { id: 'u2', username: 'bob' }])
    expect(mod.knownUsernames()).toEqual(['John Doe', 'bob'])
  })

  it('跳过缺少 id 或 username 的项', () => {
    mod.seedUsers([{ id: 'u1' }, { username: 'x' }, null, undefined, { id: 'u2', username: 'ok' }])
    expect(mod.peekUserId('x')).toBeUndefined()
    expect(mod.knownUsernames()).toEqual(['ok'])
  })

  it('seedUsers 默认参数', () => {
    expect(() => mod.seedUsers()).not.toThrow()
    expect(mod.knownUsernames()).toEqual([])
  })

  it('seedContentMentions 从内容载体批量回灌', () => {
    mod.seedContentMentions([
      { id: 'p1', mentions: [{ id: 'u1', username: 'alice' }] },
      { id: 'p2', mentions: [] },
      { id: 'p3' },
      null
    ])
    expect(mod.peekUserId('alice')).toBe('u1')
    expect(mod.knownUsernames()).toEqual(['alice'])
  })

  it.each([undefined, null, 'x', { mentions: [] }])('seedContentMentions 容错非数组入参 %p', (v) => {
    expect(() => mod.seedContentMentions(v)).not.toThrow()
    expect(mod.knownUsernames()).toEqual([])
  })

  it('peekUserId 未知 / 空入参返回 undefined', () => {
    expect(mod.peekUserId('nobody')).toBeUndefined()
    expect(mod.peekUserId(undefined)).toBeUndefined()
  })
})
