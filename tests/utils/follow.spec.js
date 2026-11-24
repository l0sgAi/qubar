import { describe, it, expect } from 'vitest'
import {
  FOLLOW_RELATION,
  FOLLOW_LIST_VISIBILITY,
  normalizeRelation,
  isFollowingRelation,
  isFollowedByRelation,
  relationLabelKey,
  isFollowLimitError,
  isFollowSelfError,
  isFollowListPrivateError,
  isFollowUserNotFoundError,
  followErrorKey
} from '@/utils/follow'
import zhCN from '@/locales/zh-CN'

const apiErr = (code, status, message = '') => Object.assign(new Error(message), { code, status })
const LIMIT = apiErr(201, 400, 'following limit reached: you can follow at most 2000 users')
const SELF = apiErr(201, 400, 'Cannot follow yourself')
const PRIVATE = apiErr(203, 403, "This user's following and follower lists are private")
const NOT_FOUND = apiErr(204, 404, 'No such user')

// 取嵌套 key，确认返回的文案 key 真实存在
const lookup = (key) => key.split('.').reduce((o, k) => o?.[k], zhCN)

describe('utils/follow', () => {
  it('枚举值与后端一致', () => {
    expect(FOLLOW_RELATION).toEqual({ NONE: 0, FOLLOWING: 1, FOLLOWED_BY: 2, MUTUAL: 3 })
    expect(FOLLOW_LIST_VISIBILITY).toEqual({ PUBLIC: 0, PRIVATE: 1 })
  })

  it('normalizeRelation：非法值按 0', () => {
    expect(normalizeRelation(3)).toBe(3)
    expect(normalizeRelation(9)).toBe(0)
    expect(normalizeRelation(undefined)).toBe(0)
  })

  it.each([
    [0, false, false, 'follow.relation.none'],
    [1, true, false, 'follow.relation.following'],
    [2, false, true, 'follow.relation.followedBy'],
    [3, true, true, 'follow.relation.mutual'],
    [null, false, false, 'follow.relation.none']
  ])('relation=%s → following=%s followedBy=%s label=%s', (rel, following, followedBy, key) => {
    expect(isFollowingRelation(rel)).toBe(following)
    expect(isFollowedByRelation(rel)).toBe(followedBy)
    expect(relationLabelKey(rel)).toBe(key)
    expect(typeof lookup(key)).toBe('string')
  })

  it('错误判断', () => {
    expect(isFollowLimitError(LIMIT)).toBe(true)
    expect(isFollowLimitError(SELF)).toBe(false)
    expect(isFollowSelfError(SELF)).toBe(true)
    expect(isFollowSelfError(LIMIT)).toBe(false)
    expect(isFollowListPrivateError(PRIVATE)).toBe(true)
    expect(isFollowUserNotFoundError(NOT_FOUND)).toBe(true)
    expect(isFollowUserNotFoundError(PRIVATE)).toBe(false)
  })

  it.each([
    [LIMIT, 'follow.error.limitReached'],
    [SELF, 'follow.error.self'],
    [PRIVATE, 'follow.error.listPrivate'],
    [NOT_FOUND, 'follow.error.userNotFound'],
    [apiErr(202, 401), 'follow.error.loginRequired'],
    [apiErr(201, 400, 'Invalid cursor'), 'follow.error.generic'],
    [new Error('Network Error'), 'follow.error.generic'],
    [undefined, 'follow.error.generic']
  ])('followErrorKey(%o) → %s（文案存在）', (err, key) => {
    expect(followErrorKey(err)).toBe(key)
    expect(typeof lookup(key)).toBe('string')
  })
})
