import { describe, it, expect, vi } from 'vitest'
import {
  DM_PERMISSION,
  DM_PERMISSION_OPTIONS,
  DM_REASON,
  DM_CONTENT_MAX,
  dmReasonKey,
  dmSendErrorReason,
  dmSendErrorKey,
  isDmSendRetryable,
  isDmBlockedReason,
  dmContentLength,
  validateDmContent,
  createClientMsgId,
  hasDmRemainingLimit,
  mergeMessagesById,
  isOwnMessage
} from '@/utils/message'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'

const apiErr = (code, status, message = '') => Object.assign(new Error(message), { code, status })
const lookup = (locale, key) => key.split('.').reduce((o, k) => o?.[k], locale)
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
// 后端 client_msg_id 规则：1–64 位字母 / 数字 / - / _
const CLIENT_MSG_ID_RE = /^[A-Za-z0-9_-]{1,64}$/

describe('utils/message 常量', () => {
  it('dm_permission 与后端一致，选项有文案', () => {
    expect(DM_PERMISSION).toEqual({ EVERYONE: 0, FOLLOWING: 1, MUTUAL: 2, CLOSED: 3 })
    expect(DM_PERMISSION_OPTIONS.map(o => o.value)).toEqual([0, 1, 2, 3])
    for (const { key } of DM_PERMISSION_OPTIONS) {
      expect(typeof lookup(zhCN, `message.permission.${key}`)).toBe('string')
      expect(typeof lookup(enUS, `message.permission.${key}`)).toBe('string')
    }
  })

  it('每个原因都有中英文文案；未知原因兜底 unknown', () => {
    for (const reason of Object.values(DM_REASON)) {
      const key = dmReasonKey(reason)
      expect(typeof lookup(zhCN, key)).toBe('string')
      expect(typeof lookup(enUS, key)).toBe('string')
    }
    expect(dmReasonKey('some_new_reason')).toBe('message.error.unknown')
    expect(dmReasonKey(DM_REASON.DM_CLOSED)).toBe('message.error.dmClosed')
  })
})

describe('dmSendErrorReason', () => {
  it.each([
    [apiErr(208, 429, 'message rate limit exceeded: at most 20 messages per minute'), DM_REASON.RATE_LIMITED],
    [apiErr(208, 429, 'new conversation limit exceeded: at most 20 new conversations per day'), DM_REASON.RATE_LIMITED],
    [apiErr(203, 403, 'This user cannot receive private messages'), DM_REASON.RECIPIENT_UNAVAILABLE],
    [apiErr(203, 403, 'dm limit reached'), DM_REASON.DM_LIMIT_REACHED],
    [apiErr(203, 403, 'Your account cannot send private messages'), DM_REASON.SENDER_DISABLED],
    [apiErr(203, 403, 'This user only accepts private messages from people they follow'), DM_REASON.FOLLOWING_ONLY],
    [apiErr(203, 403, 'This user only accepts private messages from mutual follows'), DM_REASON.MUTUAL_ONLY],
    [apiErr(203, 403, 'This user has turned off private messages'), DM_REASON.DM_CLOSED],
    [apiErr(203, 403, 'something else'), DM_REASON.UNKNOWN],
    [apiErr(204, 404, 'No such user'), DM_REASON.RECIPIENT_NOT_FOUND],
    [apiErr(204, 404, 'No such conversation'), DM_REASON.CONVERSATION_NOT_FOUND],
    [apiErr(201, 400, 'Message content is empty'), DM_REASON.CONTENT_EMPTY],
    [apiErr(201, 400, 'message content is too long: at most 2000 characters'), DM_REASON.CONTENT_TOO_LONG],
    [apiErr(201, 400, 'Cannot send message to yourself'), DM_REASON.UNKNOWN],
    [apiErr(202, 401, 'Token not found'), DM_REASON.UNKNOWN],
    [apiErr(500, 500, 'internal'), DM_REASON.NETWORK],
    [Object.assign(new Error('timeout of 10000ms exceeded'), { code: 'ECONNABORTED' }), DM_REASON.NETWORK],
    [apiErr(999, 418), DM_REASON.UNKNOWN],
    [null, DM_REASON.UNKNOWN]
  ])('%o → %s', (err, reason) => {
    expect(dmSendErrorReason(err)).toBe(reason)
  })

  it('dmSendErrorKey / isDmSendRetryable', () => {
    expect(dmSendErrorKey(apiErr(208, 429))).toBe('message.error.rateLimited')
    expect(isDmSendRetryable(new Error('Network Error'))).toBe(true)
    expect(isDmSendRetryable(apiErr(500, 502))).toBe(true)
    expect(isDmSendRetryable(apiErr(208, 429))).toBe(false) // 429 不自动重试
    expect(isDmSendRetryable(apiErr(203, 403, 'dm limit reached'))).toBe(false)
  })

  it('isDmBlockedReason：权限 / 限制 / 对方不可达应置灰', () => {
    expect(isDmBlockedReason(DM_REASON.DM_LIMIT_REACHED)).toBe(true)
    expect(isDmBlockedReason(DM_REASON.FOLLOWING_ONLY)).toBe(true)
    expect(isDmBlockedReason(DM_REASON.RATE_LIMITED)).toBe(false)
    expect(isDmBlockedReason(DM_REASON.NETWORK)).toBe(false)
  })
})

describe('正文校验', () => {
  it('按码点计数：emoji 算 1', () => {
    expect(dmContentLength('你好')).toBe(2)
    expect(dmContentLength('😀a')).toBe(2)
    expect(dmContentLength(null)).toBe(0)
  })

  it('空 / 全空白 / 超长', () => {
    expect(validateDmContent('')).toBe(DM_REASON.CONTENT_EMPTY)
    expect(validateDmContent(' \n\t ')).toBe(DM_REASON.CONTENT_EMPTY)
    expect(validateDmContent(undefined)).toBe(DM_REASON.CONTENT_EMPTY)
    expect(validateDmContent('😀'.repeat(DM_CONTENT_MAX))).toBeNull()
    expect(validateDmContent('a'.repeat(DM_CONTENT_MAX + 1))).toBe(DM_REASON.CONTENT_TOO_LONG)
    expect(validateDmContent(' hi\n')).toBeNull()
  })
})

describe('createClientMsgId', () => {
  it('优先 crypto.randomUUID', () => {
    const id = createClientMsgId()
    expect(id).toMatch(CLIENT_MSG_ID_RE)
    expect(createClientMsgId()).not.toBe(id)
  })

  it('没有 randomUUID 时用 getRandomValues 拼 v4', () => {
    vi.stubGlobal('crypto', { getRandomValues: (arr) => arr.fill(0xab) })
    const id = createClientMsgId()
    expect(id).toMatch(UUID_RE)
    expect(id).toMatch(CLIENT_MSG_ID_RE)
  })

  it('没有 crypto 时用 Math.random 兜底', () => {
    vi.stubGlobal('crypto', undefined)
    expect(createClientMsgId()).toMatch(UUID_RE)
  })
})

describe('其它', () => {
  it('hasDmRemainingLimit：数字（含 0）才提示，null 不受限', () => {
    expect(hasDmRemainingLimit(4)).toBe(true)
    expect(hasDmRemainingLimit(0)).toBe(true)
    expect(hasDmRemainingLimit(null)).toBe(false)
    expect(hasDmRemainingLimit(undefined)).toBe(false)
  })

  it('mergeMessagesById：去重、后者覆盖、输出旧 → 新', () => {
    const page = [ // 接口返回新 → 旧
      { id: '019a7b10-0003', content: 'c' },
      { id: '019a7b10-0002', content: 'b' }
    ]
    const existing = [{ id: '019a7b10-0001', content: 'a' }, { id: '019a7b10-0002', content: 'b-short' }]
    const merged = mergeMessagesById(existing, page)
    expect(merged.map(m => m.id)).toEqual(['019a7b10-0001', '019a7b10-0002', '019a7b10-0003'])
    expect(merged[1].content).toBe('b')
    expect(mergeMessagesById(undefined, [null, { content: 'no id' }])).toEqual([])
    expect(mergeMessagesById([{ id: 'x' }, { id: 'x' }])).toEqual([{ id: 'x' }])
  })

  it('isOwnMessage', () => {
    expect(isOwnMessage({ sender_id: 'me' }, 'me')).toBe(true)
    expect(isOwnMessage({ sender_id: 'u2' }, 'me')).toBe(false)
    expect(isOwnMessage(null, 'me')).toBe(false)
    expect(isOwnMessage({ sender_id: undefined }, undefined)).toBe(false)
  })
})
