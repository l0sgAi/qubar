import { describe, it, expect } from 'vitest'
import {
  CIRCLE_ROLE, JOIN_TYPE,
  normalizeJoinType, isPrivateJoinType, needsInviteToJoin,
  joinErrorKey, isForbiddenError, canModerate
} from '@/constants/circle'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

const err = (fields) => Object.assign(new Error('x'), fields)
const { MEMBER, ADMIN, OWNER } = CIRCLE_ROLE

describe('constants/circle：加入方式（后端 #168 下线需审核）', () => {
  it('normalizeJoinType：0 / 缺省为公开，其余（含已下线的 1）一律私密', () => {
    expect(normalizeJoinType(0)).toBe(JOIN_TYPE.DIRECT)
    expect(normalizeJoinType(undefined)).toBe(JOIN_TYPE.DIRECT)
    expect(normalizeJoinType(null)).toBe(JOIN_TYPE.DIRECT)
    expect(normalizeJoinType(1)).toBe(JOIN_TYPE.PRIVATE)
    expect(normalizeJoinType(2)).toBe(JOIN_TYPE.PRIVATE)
    expect(isPrivateJoinType(1)).toBe(true)
    expect(isPrivateJoinType(0)).toBe(false)
  })

  it('needsInviteToJoin：只有私密圈的非成员（含访客、restricted 精简详情）需要邀请', () => {
    expect(needsInviteToJoin({ join_type: 2, restricted: true })).toBe(true)
    expect(needsInviteToJoin({ join_type: 1, is_joined: false })).toBe(true)
    expect(needsInviteToJoin({ join_type: 2, is_joined: true })).toBe(false)
    expect(needsInviteToJoin({ join_type: 0, is_joined: false })).toBe(false)
    expect(needsInviteToJoin(null)).toBe(false)
  })
})

describe('constants/circle：加入失败与 403 识别', () => {
  it('joinErrorKey：信封 code 与 HTTP status 都认', () => {
    expect(joinErrorKey(err({ code: 207 }), 0)).toBe('circle.joinError.alreadyMember')
    expect(joinErrorKey(err({ status: 409 }), 0)).toBe('circle.joinError.alreadyMember')
    expect(joinErrorKey(err({ code: 204 }), 0)).toBe('circle.joinError.notFound')
    expect(joinErrorKey(err({ code: 210 }), 0)).toBe('circle.joinError.generic')
    expect(joinErrorKey(new Error('Network Error'), 0)).toBe('circle.joinError.generic')
  })

  it('joinErrorKey：403 按加入方式区分「需邀请」和「已被拉黑」', () => {
    expect(joinErrorKey(err({ code: 203 }), 2)).toBe('circle.joinError.inviteOnly')
    expect(joinErrorKey(err({ status: 403 }), 1)).toBe('circle.joinError.inviteOnly')
    expect(joinErrorKey(err({ code: 203 }), 0)).toBe('circle.joinError.banned')
  })

  it('joinErrorKey 的每个 key 两种语言都有文案', () => {
    for (const k of ['alreadyMember', 'inviteOnly', 'banned', 'notFound', 'generic']) {
      expect(zhCN.circle.joinError[k]).toBeTruthy()
      expect(enUS.circle.joinError[k]).toBeTruthy()
    }
  })

  it('isForbiddenError', () => {
    expect(isForbiddenError(err({ code: 203 }))).toBe(true)
    expect(isForbiddenError(err({ status: 403 }))).toBe(true)
    expect(isForbiddenError(err({ code: 204 }))).toBe(false)
    expect(isForbiddenError(undefined)).toBe(false)
  })
})

describe('constants/circle：移出 / 拉黑等管理操作的权限矩阵（文档 §三）', () => {
  it('圈主可操作管理员和普通成员，不能操作自己（圈主）', () => {
    expect(canModerate(OWNER, ADMIN)).toBe(true)
    expect(canModerate(OWNER, MEMBER)).toBe(true)
    expect(canModerate(OWNER, OWNER)).toBe(false)
  })

  it('管理员只能操作普通成员', () => {
    expect(canModerate(ADMIN, MEMBER)).toBe(true)
    expect(canModerate(ADMIN, ADMIN)).toBe(false)
    expect(canModerate(ADMIN, OWNER)).toBe(false)
  })

  it('普通成员不能操作任何人', () => {
    expect(canModerate(MEMBER, MEMBER)).toBe(false)
  })
})
