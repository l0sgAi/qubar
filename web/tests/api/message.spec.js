import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import MockAdapter from 'axios-mock-adapter'
import request from '@/utils/request'
import {
  sendMessage,
  getConversations,
  getConversationWith,
  getConversationMessages,
  markConversationRead,
  deleteConversation,
  getDmUnreadCount
} from '@/api/message'
import { sharePost } from '@/api/post'
import { getUserSettings, updateUserSettings } from '@/api/user'

const UID = '019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10'
const CID = '019a7b10-2c3a-7d11-9e22-3f4a5b6c7d8e'

describe('api/message', () => {
  let mock

  beforeEach(() => {
    mock = new MockAdapter(request)
  })

  afterEach(() => {
    mock.restore()
  })

  it('发送：请求体原样带 receiver_id / content / client_msg_id', async () => {
    mock.onPost('/message/send').reply(200, { code: 200, data: { id: 'm1', conversation_id: CID, remaining_count: 4 } })
    const body = { receiver_id: UID, content: '你好', client_msg_id: 'k-1' }
    const res = await sendMessage(body)
    expect(JSON.parse(mock.history.post[0].data)).toEqual(body)
    expect(res.data.remaining_count).toBe(4)
  })

  it('发送 429：reject 带 code 208 / status 429', async () => {
    mock.onPost('/message/send').reply(429, { code: 208, message: 'message rate limit exceeded: at most 20 messages per minute' })
    await expect(sendMessage({ receiver_id: UID, content: 'x' })).rejects.toMatchObject({ code: 208, status: 429 })
  })

  it('会话列表 / 按对方查会话 / 消息历史', async () => {
    mock.onGet('/message/conversations').reply(200, { code: 200, data: { items: [], has_more: false, next_cursor: '' } })
    mock.onGet(`/message/conversations/with/${UID}`).reply(200, { code: 200, data: { conversation: null, can_send: true, cannot_send_reason: null } })
    mock.onGet(`/message/conversations/${CID}/messages`).reply(200, { code: 200, data: { items: [], has_more: false, next_cursor: '', last_read_message_id: '' } })

    await getConversations({ size: 20 })
    const withRes = await getConversationWith(UID)
    await getConversationMessages(CID, { cursor: 'c1' })

    expect(mock.history.get[0].params).toEqual({ size: 20 })
    expect(withRes.data.can_send).toBe(true)
    expect(mock.history.get[2].params).toEqual({ cursor: 'c1' })
  })

  it('标记已读 / 删除会话 / 未读总数', async () => {
    mock.onPost(`/message/conversations/${CID}/read`).reply(200, { code: 200, data: null })
    mock.onDelete(`/message/conversations/${CID}`).reply(200, { code: 200, data: null })
    mock.onGet('/message/unread-count').reply(200, { code: 200, data: { unread_count: 3 } })

    await markConversationRead(CID)
    await deleteConversation(CID)
    expect((await getDmUnreadCount()).data.unread_count).toBe(3)
    expect(mock.history.post[0].url).toBe(`/message/conversations/${CID}/read`)
    expect(mock.history.delete[0].url).toBe(`/message/conversations/${CID}`)
  })
})

describe('api/post sharePost', () => {
  let mock
  beforeEach(() => { mock = new MockAdapter(request) })
  afterEach(() => { mock.restore() })

  it('POST /post/share { post_id }', async () => {
    mock.onPost('/post/share').reply(200, { code: 200, data: { share_count: 8, first_share: true } })
    const res = await sharePost('p1')
    expect(JSON.parse(mock.history.post[0].data)).toEqual({ post_id: 'p1' })
    expect(res.data).toEqual({ share_count: 8, first_share: true })
  })

  it('503 / 212：reject 带 status，便于判断可重试', async () => {
    mock.onPost('/post/share').reply(503, { code: 212, message: 'share event publish failed' })
    await expect(sharePost('p1')).rejects.toMatchObject({ code: 212, status: 503 })
  })
})

describe('api/user settings', () => {
  let mock
  beforeEach(() => { mock = new MockAdapter(request) })
  afterEach(() => { mock.restore() })

  it('GET /user/settings', async () => {
    mock.onGet('/user/settings').reply(200, { code: 200, data: { follow_list_visibility: 0, dm_permission: 0 } })
    expect((await getUserSettings()).data).toEqual({ follow_list_visibility: 0, dm_permission: 0 })
  })

  it('PUT /user/settings 部分更新：只提交传入且非空的项', async () => {
    mock.onPut('/user/settings').reply(200, { code: 200, data: { follow_list_visibility: 1, dm_permission: 0 } })
    await updateUserSettings({ follow_list_visibility: 1 })
    await updateUserSettings({ follow_list_visibility: null, dm_permission: 0, extra: 'x' })
    await updateUserSettings()
    const bodies = mock.history.put.map(r => JSON.parse(r.data))
    expect(bodies).toEqual([
      { follow_list_visibility: 1 },
      { dm_permission: 0 },
      {}
    ])
  })
})
