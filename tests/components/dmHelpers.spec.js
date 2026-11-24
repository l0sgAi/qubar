import { describe, it, expect } from 'vitest'
import {
  toMessageVO,
  sentToMessageVO,
  bumpConversation,
  sumUnread,
  isLastMessageMine,
  newMessagesDividerId,
  needsTimeLabel,
  formatChatTime
} from '@/components/message/dmHelpers'

const conv = (id, peer, lastId, unread = 0, sender = peer) => ({
  conversation_id: id,
  peer_id: peer,
  peer: { id: peer, username: peer },
  last_message: { id: lastId, sender_id: sender, msg_type: 1, content: 'x', create_time: '2026-11-01T00:00:00Z' },
  unread_count: unread
})

describe('dmHelpers', () => {
  it('toMessageVO 去掉 sender / truncated，sentToMessageVO 去掉 remaining_count', () => {
    const sse = { id: 'm1', conversation_id: 'c', sender_id: 'u', sender: { id: 'u' }, msg_type: 1, content: 'hi', truncated: false, create_time: 't' }
    expect(toMessageVO(sse)).toEqual({ id: 'm1', conversation_id: 'c', sender_id: 'u', msg_type: 1, content: 'hi', create_time: 't' })
    expect(toMessageVO(null)).toBeNull()
    expect(sentToMessageVO({ id: 'm2', content: 'a', remaining_count: 3 })).toEqual({ id: 'm2', content: 'a' })
    expect(sentToMessageVO(undefined)).toBeNull()
  })

  it('bumpConversation：命中的会话置顶、更新摘要、对方消息未读 +1', () => {
    const list = [conv('c1', 'a', '01', 0), conv('c2', 'b', '02', 1)]
    const msg = { id: '05', conversation_id: 'c2', sender_id: 'b', msg_type: 1, content: 'new', create_time: 't2' }
    const { list: next, found } = bumpConversation(list, msg)
    expect(found).toBe(true)
    expect(next.map(c => c.conversation_id)).toEqual(['c2', 'c1'])
    expect(next[0].last_message).toEqual({ id: '05', sender_id: 'b', msg_type: 1, content: 'new', create_time: 't2' })
    expect(next[0].unread_count).toBe(2)
    expect(list[1].unread_count).toBe(1) // 不改原数组
  })

  it('bumpConversation：重复 / 更旧的消息不重复计未读；我发的不计未读；没命中返回 found=false', () => {
    const list = [conv('c1', 'a', '01'), conv('c2', 'b', '09', 1)]
    const dup = bumpConversation(list, { id: '09', conversation_id: 'c2', sender_id: 'b' })
    expect(dup.list[0].unread_count).toBe(1)
    const mine = bumpConversation(list, { id: '10', conversation_id: 'c1', sender_id: 'me' })
    expect(mine.list[0].unread_count).toBe(0)
    const noLast = bumpConversation([{ conversation_id: 'c3', peer_id: 'z', last_message: null }], { id: '1', conversation_id: 'c3', sender_id: 'z' })
    expect(noLast.list[0].unread_count).toBe(1)
    expect(bumpConversation(list, { id: '11', conversation_id: 'nope' })).toEqual({ list, found: false })
  })

  it('sumUnread / isLastMessageMine', () => {
    expect(sumUnread([conv('c1', 'a', '1', 2), conv('c2', 'b', '2', 3), { unread_count: undefined }])).toBe(5)
    expect(isLastMessageMine(conv('c1', 'a', '1', 0, 'me'))).toBe(true)
    expect(isLastMessageMine(conv('c1', 'a', '1'))).toBe(false)
    expect(isLastMessageMine({ peer_id: 'a', last_message: null })).toBe(false)
  })

  it('newMessagesDividerId：第一条比已读位置新的对方消息；在第一条或没有时不画', () => {
    const msgs = [
      { id: '01', sender_id: 'p' },
      { id: '02', sender_id: 'me' },
      { id: '03', sender_id: 'me' },
      { id: '04', sender_id: 'p' },
      { id: '05', sender_id: 'p' }
    ]
    expect(newMessagesDividerId(msgs, '02', 'p')).toBe('04')
    expect(newMessagesDividerId(msgs, '05', 'p')).toBeNull()
    expect(newMessagesDividerId(msgs, '', 'p')).toBeNull() // 从未读过：第一条就是新消息
    expect(newMessagesDividerId(msgs, null, 'p')).toBeNull()
  })

  it('needsTimeLabel：首条、间隔超过 5 分钟时需要', () => {
    const a = { create_time: '2026-11-01T10:00:00Z' }
    const b = { create_time: '2026-11-01T10:04:00Z' }
    const c = { create_time: '2026-11-01T10:10:00Z' }
    expect(needsTimeLabel(undefined, a)).toBe(true)
    expect(needsTimeLabel(a, b)).toBe(false)
    expect(needsTimeLabel(b, c)).toBe(true)
    expect(needsTimeLabel(a, null)).toBe(false)
    expect(needsTimeLabel({ create_time: 'bad' }, a)).toBe(false)
  })

  it('formatChatTime：今天只显示时分，跨年带年份，非法时间为空', () => {
    const now = new Date(2026, 10, 24, 15, 0)
    expect(formatChatTime(new Date(2026, 10, 24, 9, 5).toISOString(), 'en-US', now)).toMatch(/09:05/)
    expect(formatChatTime(new Date(2026, 0, 2, 9, 5).toISOString(), 'en-US', now)).not.toMatch(/2026/)
    expect(formatChatTime(new Date(2025, 0, 2, 9, 5).toISOString(), 'en-US', now)).toMatch(/2025/)
    expect(formatChatTime('bad', 'en-US', now)).toBe('')
  })
})
