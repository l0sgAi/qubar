import { describe, it, expect } from 'vitest'
import {
  INVITE_LINK_STATUS,
  inviteLinkStatus,
  canManageInvites,
  invitePath,
  inviteUrl,
  acceptInviteErrorKey,
  isInviteInvalidError,
  parseEmailInput,
  groupIgnoredEmails,
  isInviteHandledError,
  respondInviteErrorKey
} from '@/constants/circleInvite'

const link = (extra = {}) => ({ revoked: false, expired: false, max_uses: null, used_count: 0, ...extra })

describe('inviteLinkStatus', () => {
  it.each([
    [link(), INVITE_LINK_STATUS.ACTIVE],
    [link({ max_uses: 10, used_count: 9 }), INVITE_LINK_STATUS.ACTIVE],
    [link({ max_uses: 10, used_count: 10 }), INVITE_LINK_STATUS.USED_UP],
    [link({ max_uses: 1, used_count: 3 }), INVITE_LINK_STATUS.USED_UP],
    [link({ used_count: 9999 }), INVITE_LINK_STATUS.ACTIVE],
    [link({ expired: true }), INVITE_LINK_STATUS.EXPIRED],
    [link({ expired: true, max_uses: 1, used_count: 1 }), INVITE_LINK_STATUS.EXPIRED],
    [link({ revoked: true, expired: true }), INVITE_LINK_STATUS.REVOKED]
  ])('%o → %s', (input, expected) => {
    expect(inviteLinkStatus(input)).toBe(expected)
  })
})

describe('canManageInvites', () => {
  it.each([
    [{ member_role: 30, join_type: 2 }, true],
    [{ member_role: 20, join_type: 2 }, true],
    // 后端缓存可能仍返回已下线的 1，按私密处理
    [{ member_role: 20, join_type: 1 }, true],
    [{ member_role: 10, join_type: 2 }, false],
    [{ member_role: 0, join_type: 2 }, false],
    [{ member_role: 30, join_type: 0 }, false],
    [null, false]
  ])('%o → %s', (circle, expected) => {
    expect(canManageInvites(circle)).toBe(expected)
  })
})

describe('invite url', () => {
  const token = 'q3vAbc-_0123456789abcdefghijklmnopqrstuvwxyz'

  it('token 原样放进 path', () => {
    expect(invitePath(token)).toBe(`/invite/${token}`)
    expect(inviteUrl(token, 'https://qubar.example')).toBe(`https://qubar.example/invite/${token}`)
  })

  it('默认取当前站点 origin', () => {
    expect(inviteUrl('t')).toBe(`${window.location.origin}/invite/t`)
  })
})

describe('acceptInviteErrorKey / isInviteInvalidError', () => {
  it.each([
    [{ code: 203 }, 'circle.invite.landing.error.banned'],
    [{ status: 403 }, 'circle.invite.landing.error.banned'],
    [{ code: 204 }, 'circle.invite.landing.error.invalid'],
    [{ status: 404 }, 'circle.invite.landing.error.invalid'],
    [{ code: 500 }, 'circle.invite.landing.error.generic'],
    [new Error('Network Error'), 'circle.invite.landing.error.generic']
  ])('%o → %s', (error, key) => {
    expect(acceptInviteErrorKey(error)).toBe(key)
  })

  it('只有 404 视为邀请失效', () => {
    expect(isInviteInvalidError({ code: 204 })).toBe(true)
    expect(isInviteInvalidError({ status: 404 })).toBe(true)
    expect(isInviteInvalidError({ code: 203, status: 403 })).toBe(false)
    expect(isInviteInvalidError(undefined)).toBe(false)
  })
})

describe('parseEmailInput', () => {
  it('按换行 / 逗号（含全角）/ 分号 / 空白分隔，去掉空项，不去重不校验', () => {
    expect(parseEmailInput(' Alice@Example.com\nbob@example.com ,carol@example.com，dan@x.io;eve@x.io；bad\n\n alice@example.com ')).toEqual([
      'Alice@Example.com', 'bob@example.com', 'carol@example.com', 'dan@x.io', 'eve@x.io', 'bad', 'alice@example.com'
    ])
  })

  it('空输入返回空数组', () => {
    expect(parseEmailInput('')).toEqual([])
    expect(parseEmailInput(' \n , ')).toEqual([])
    expect(parseEmailInput(undefined)).toEqual([])
  })
})

describe('groupIgnoredEmails', () => {
  it('按 invalid / duplicate / already_invited 顺序分组，只保留非空组', () => {
    expect(groupIgnoredEmails([
      { email: 'a@x.io', reason: 'already_invited' },
      { email: 'bad', reason: 'invalid' },
      { email: 'a@x.io', reason: 'already_invited' },
      { email: 'Bad2', reason: 'invalid' }
    ])).toEqual([
      { reason: 'invalid', emails: ['bad', 'Bad2'] },
      { reason: 'already_invited', emails: ['a@x.io', 'a@x.io'] }
    ])
  })

  it('未知原因归入 invalid；空 / 缺省返回空数组', () => {
    expect(groupIgnoredEmails([{ email: 'x', reason: 'weird' }])).toEqual([{ reason: 'invalid', emails: ['x'] }])
    expect(groupIgnoredEmails([])).toEqual([])
    expect(groupIgnoredEmails(undefined)).toEqual([])
  })
})

describe('isInviteHandledError / respondInviteErrorKey', () => {
  it('409 / 207 视为已处理', () => {
    expect(isInviteHandledError({ code: 207 })).toBe(true)
    expect(isInviteHandledError({ status: 409 })).toBe(true)
    expect(isInviteHandledError({ code: 204 })).toBe(false)
  })

  it.each([
    [{ code: 204 }, 'circle.invite.mine.error.invalid'],
    [{ status: 404 }, 'circle.invite.mine.error.invalid'],
    [{ code: 203 }, 'circle.invite.mine.error.banned'],
    [{ status: 403 }, 'circle.invite.mine.error.banned'],
    [new Error('Network Error'), 'circle.invite.mine.error.generic']
  ])('%o → %s', (error, key) => {
    expect(respondInviteErrorKey(error)).toBe(key)
  })
})
