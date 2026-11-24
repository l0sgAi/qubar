import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, computed } from 'vue'

const api = vi.hoisted(() => ({}))
const notice = vi.hoisted(() => ({ handler: null, dm: null, refresh: null }))
const toast = vi.hoisted(() => ({}))

vi.mock('@/api/message', () => ({
  getConversationWith: (...a) => api.getConversationWith(...a),
  getConversationMessages: (...a) => api.getConversationMessages(...a),
  markConversationRead: (...a) => api.markConversationRead(...a),
  sendMessage: (...a) => api.sendMessage(...a)
}))
vi.mock('@/api/user', () => ({ getUserDetail: (...a) => api.getUserDetail(...a) }))
vi.mock('@/composables/useUnreadNotice', () => ({
  useUnreadNotice: ({ onDmMessage } = {}) => {
    notice.handler = onDmMessage
    return { dmUnreadCount: notice.dm, dmSyncVersion: notice.sync }
  },
  refreshDmUnreadCount: (...a) => notice.refresh(...a)
}))
vi.mock('@/composables/useAppShell', () => ({
  useAppShell: () => ({ offset: ref(0), mobileShell: computed(() => false), contentStyle: computed(() => ({})) })
}))
vi.mock('@/components/layout/AppShell.vue', () => ({ default: { render: () => null } }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({
    error: (...a) => toast.error(...a),
    warning: (...a) => toast.warning(...a),
    info: (...a) => toast.info(...a),
    success: vi.fn()
  })
}))

const { default: ChatView } = await import('@/views/message/ChatView.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const PEER = 'peer-1'
const ME = 'me-1'
const CONV = 'conv-1'
const msg = (id, sender, content = `m${id}`) => ({
  id, conversation_id: CONV, sender_id: sender, msg_type: 1, content, create_time: '2026-11-24T10:00:00Z'
})
const conversation = (extra = {}) => ({
  conversation_id: CONV,
  type: 1,
  peer_id: PEER,
  peer: { id: PEER, username: 'alice', avatar_url: '' },
  last_message: msg('03', PEER),
  unread_count: 1,
  ...extra
})

const mountChat = async () => {
  const router = createTestRouter([
    { path: '/messages/:userId', name: 'message-chat' },
    { path: '/user/:id' }
  ])
  await router.push(`/messages/${PEER}`)
  const wrapper = mount(ChatView, { global: { plugins: [router, createTestI18n()] } })
  await flushPromises()
  return wrapper
}

const typeAndSend = async (wrapper, text) => {
  await wrapper.find('textarea').setValue(text)
  await wrapper.find('.send-btn').trigger('click')
}

const bubbles = (wrapper) => wrapper.findAll('.chat-bubble').map(el => el.text())

const networkError = () => Object.assign(new Error('timeout of 10000ms exceeded'), { code: 'ECONNABORTED' })

describe('ChatView', () => {
  beforeEach(() => {
    notice.dm = ref(0)
    notice.sync = ref(0)
    notice.refresh = vi.fn()
    notice.handler = null
    toast.error = vi.fn()
    toast.warning = vi.fn()
    toast.info = vi.fn()
    api.getConversationWith = vi.fn(() => Promise.resolve({ data: { conversation: null, can_send: true, cannot_send_reason: null } }))
    api.getConversationMessages = vi.fn(() => Promise.resolve({ data: { items: [], has_more: false, next_cursor: '', last_read_message_id: '' } }))
    api.markConversationRead = vi.fn(() => Promise.resolve({}))
    api.getUserDetail = vi.fn(() => Promise.resolve({ data: { id: PEER, username: 'alice', avatar_url: '', role: 0 } }))
    api.sendMessage = vi.fn(({ content }) => Promise.resolve({ data: { ...msg('10', ME, content), remaining_count: null } }))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.spyOn(console, 'warn').mockImplementation(() => {})
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('没聊过：空白聊天页，取对方资料；发出第一条后用响应的 conversation_id，提示剩余条数', async () => {
    api.sendMessage = vi.fn(({ content }) => Promise.resolve({ data: { ...msg('10', ME, content), remaining_count: 4 } }))
    const wrapper = await mountChat()
    expect(api.getConversationWith).toHaveBeenCalledWith(PEER)
    expect(api.getUserDetail).toHaveBeenCalledWith(PEER)
    expect(api.getConversationMessages).not.toHaveBeenCalled()
    expect(wrapper.find('.chat-empty').exists()).toBe(true)
    expect(wrapper.find('.peer-name').text()).toBe('alice')

    await typeAndSend(wrapper, '你好')
    await flushPromises()
    const body = api.sendMessage.mock.calls[0][0]
    expect(body.receiver_id).toBe(PEER)
    expect(body.content).toBe('你好')
    expect(body.client_msg_id).toMatch(/^[0-9a-f-]{36}$/)
    expect(bubbles(wrapper)).toEqual(['你好'])
    expect(wrapper.find('.composer-hint').text()).toContain('4')

    // 之后对方发来消息：会话已建立，直接上屏并标记已读
    notice.handler({ ...msg('11', PEER, '回复'), sender: { username: 'alice' }, truncated: false })
    await flushPromises()
    expect(bubbles(wrapper)).toEqual(['你好', '回复'])
    expect(api.markConversationRead).toHaveBeenCalledWith(CONV)
  })

  it('有会话：拉第一页（新 → 旧）反转渲染，画新消息分隔线，标记已读并刷新角标', async () => {
    api.getConversationWith = vi.fn(() => Promise.resolve({ data: { conversation: conversation(), can_send: true, cannot_send_reason: null } }))
    api.getConversationMessages = vi.fn(() => Promise.resolve({
      data: { items: [msg('03', PEER), msg('02', ME), msg('01', PEER)], has_more: true, next_cursor: 'c1', last_read_message_id: '02' }
    }))
    const wrapper = await mountChat()
    expect(bubbles(wrapper)).toEqual(['m01', 'm02', 'm03'])
    expect(wrapper.findAll('.chat-row').map(r => r.classes().includes('mine'))).toEqual([false, true, false])
    expect(wrapper.find('.chat-divider').exists()).toBe(true)
    expect(api.markConversationRead).toHaveBeenCalledWith(CONV)
    expect(notice.refresh).toHaveBeenCalled()
    expect(wrapper.find('.older-btn').exists()).toBe(true)

    api.getConversationMessages = vi.fn(() => Promise.resolve({ data: { items: [msg('00', ME)], has_more: false, next_cursor: '' } }))
    await wrapper.find('.older-btn').trigger('click')
    await flushPromises()
    expect(api.getConversationMessages).toHaveBeenCalledWith(CONV, { cursor: 'c1', size: 20 })
    expect(bubbles(wrapper)).toEqual(['m00', 'm01', 'm02', 'm03'])
    expect(wrapper.find('.older-btn').exists()).toBe(false)
  })

  it('can_send=false：进入即置灰输入框并显示原因', async () => {
    api.getConversationWith = vi.fn(() => Promise.resolve({ data: { conversation: null, can_send: false, cannot_send_reason: 'dm_closed' } }))
    const wrapper = await mountChat()
    expect(wrapper.find('textarea').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.composer-hint').text()).toBe('对方已关闭私信')
  })

  it('对方已注销：显示「已注销用户」且不能发送', async () => {
    api.getConversationWith = vi.fn(() => Promise.resolve({
      data: { conversation: conversation({ peer: null }), can_send: false, cannot_send_reason: 'recipient_not_found' }
    }))
    const wrapper = await mountChat()
    expect(wrapper.find('.peer-link.deleted').text()).toContain('已注销用户')
    expect(wrapper.find('textarea').attributes('disabled')).toBeDefined()
  })

  it('网络错误 / 超时用同一个 client_msg_id 自动重试，只出现一条', async () => {
    vi.useFakeTimers()
    api.sendMessage = vi.fn()
      .mockRejectedValueOnce(networkError())
      .mockRejectedValueOnce(Object.assign(new Error('Internal'), { code: 500, status: 500 }))
      .mockImplementationOnce(({ content }) => Promise.resolve({ data: { ...msg('10', ME, content), remaining_count: null } }))
    const wrapper = await mountChat()
    await typeAndSend(wrapper, 'hi')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1000)
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(api.sendMessage).toHaveBeenCalledTimes(3)
    const ids = api.sendMessage.mock.calls.map(c => c[0].client_msg_id)
    expect(new Set(ids).size).toBe(1)
    expect(bubbles(wrapper)).toEqual(['hi'])
    expect(wrapper.find('.chat-bubble.pending').exists()).toBe(false)
  })

  it('429 不自动重试：提示「发送太频繁」，手动重试复用同一个键', async () => {
    api.sendMessage = vi.fn().mockRejectedValueOnce(Object.assign(new Error('message rate limit exceeded'), { code: 208, status: 429 }))
    const wrapper = await mountChat()
    await typeAndSend(wrapper, 'hi')
    await flushPromises()
    expect(api.sendMessage).toHaveBeenCalledTimes(1)
    expect(toast.warning).toHaveBeenCalledWith('发送太频繁，请稍后再试')
    expect(wrapper.find('.chat-bubble.failed').exists()).toBe(true)

    api.sendMessage.mockImplementationOnce(({ content }) => Promise.resolve({ data: { ...msg('10', ME, content), remaining_count: null } }))
    const retry = wrapper.findAll('.pending-action').find(b => b.text() === '重试')
    await retry.trigger('click')
    await flushPromises()
    expect(api.sendMessage.mock.calls[1][0].client_msg_id).toBe(api.sendMessage.mock.calls[0][0].client_msg_id)
    expect(bubbles(wrapper)).toEqual(['hi'])
  })

  it('remaining_count=0：置灰输入框并提示对方关注或回复后可继续；403 权限拒绝同样置灰', async () => {
    api.sendMessage = vi.fn(({ content }) => Promise.resolve({ data: { ...msg('10', ME, content), remaining_count: 0 } }))
    const wrapper = await mountChat()
    await typeAndSend(wrapper, '第五条')
    await flushPromises()
    expect(wrapper.find('textarea').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.composer-hint').text()).toBe('对方关注你或回复后可继续发送')

    // 对方回复：重新查询，限制解除
    api.getConversationWith = vi.fn(() => Promise.resolve({ data: { conversation: conversation(), can_send: true, cannot_send_reason: null } }))
    notice.handler({ ...msg('11', PEER), truncated: false })
    await flushPromises()
    expect(wrapper.find('textarea').attributes('disabled')).toBeUndefined()

    api.sendMessage = vi.fn().mockRejectedValueOnce(Object.assign(new Error('This user has turned off private messages'), { code: 203, status: 403 }))
    await typeAndSend(wrapper, 'again')
    await flushPromises()
    expect(wrapper.find('.composer-hint').text()).toBe('对方已关闭私信')
    expect(wrapper.find('.pending-error').text()).toBe('对方已关闭私信')
  })

  it('空白内容不发送', async () => {
    const wrapper = await mountChat()
    await typeAndSend(wrapper, '   \n ')
    await flushPromises()
    expect(api.sendMessage).not.toHaveBeenCalled()
  })

  it('SSE：truncated 重新拉第一页；别的会话弹提示', async () => {
    api.getConversationWith = vi.fn(() => Promise.resolve({ data: { conversation: conversation(), can_send: true, cannot_send_reason: null } }))
    api.getConversationMessages = vi.fn(() => Promise.resolve({ data: { items: [msg('03', PEER)], has_more: false, next_cursor: '', last_read_message_id: '03' } }))
    const wrapper = await mountChat()
    expect(api.getConversationMessages).toHaveBeenCalledTimes(1)

    api.getConversationMessages = vi.fn(() => Promise.resolve({ data: { items: [msg('04', PEER, 'x'.repeat(150)), msg('03', PEER)], has_more: false, next_cursor: '' } }))
    notice.handler({ ...msg('04', PEER, 'x'.repeat(100)), truncated: true })
    notice.handler({ ...msg('04', PEER, 'x'.repeat(100)), truncated: true })
    await flushPromises()
    expect(api.getConversationMessages).toHaveBeenCalledWith(CONV, { size: 20 })
    expect(api.getConversationMessages).toHaveBeenCalledTimes(1)
    expect(bubbles(wrapper)[1]).toHaveLength(150)

    notice.handler({ ...msg('05', 'other'), conversation_id: 'conv-2', sender: { username: 'bob' }, truncated: false })
    await flushPromises()
    expect(toast.info).toHaveBeenCalledWith('bob：m05')
    expect(bubbles(wrapper)).toHaveLength(2)
  })

  it('SSE 断开时私信未读数（轮询）增加：主动拉第一页', async () => {
    api.getConversationWith = vi.fn(() => Promise.resolve({ data: { conversation: conversation(), can_send: true, cannot_send_reason: null } }))
    api.getConversationMessages = vi.fn(() => Promise.resolve({ data: { items: [msg('03', PEER)], has_more: false, next_cursor: '', last_read_message_id: '03' } }))
    const wrapper = await mountChat()
    api.getConversationMessages = vi.fn(() => Promise.resolve({ data: { items: [msg('04', PEER), msg('03', PEER)], has_more: false, next_cursor: '' } }))
    notice.dm.value = 1
    await flushPromises()
    expect(bubbles(wrapper)).toEqual(['m03', 'm04'])
  })

  it('切换 peer 后旧“更早”请求不影响新聊天的分页、内容和读状态', async () => {
    api.getConversationWith.mockImplementation(async id => ({
      data: { conversation: conversation({ conversation_id: id, peer_id: id }), can_send: true }
    }))
    let finishOld
    api.getConversationMessages.mockImplementation(async (id, { cursor }) => {
      if (id === PEER && cursor) return new Promise(resolve => { finishOld = resolve })
      return { data: {
        items: [msg(cursor ? '01' : '02', id, id + (cursor || ''))],
        has_more: !cursor, next_cursor: id + '-cursor'
      } }
    })
    const wrapper = await mountChat()
    await wrapper.get('.older-btn').trigger('click')
    await wrapper.vm.$router.push('/messages/bob')
    await flushPromises()
    finishOld({ data: { items: [msg('00', PEER, '旧数据')], has_more: false } })
    await flushPromises()
    await wrapper.get('.older-btn').trigger('click')
    await flushPromises()
    expect(api.getConversationMessages).toHaveBeenLastCalledWith('bob', { cursor: 'bob-cursor', size: 20 })
    expect(bubbles(wrapper)).toEqual(['bobbob-cursor', 'bob'])
    expect(api.markConversationRead.mock.calls).toEqual([[PEER], ['bob']])
    expect(wrapper.find('textarea').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })

  it.each([
    [true, null], [false, 'dm_closed'], [false, 'recipient_not_found']
  ])('只有轮询收到首条回复：输入框依据后端 can_send=%s / reason=%s', async (allowed, reason) => {
    api.getConversationWith.mockResolvedValueOnce({ data: {
      conversation: conversation(), can_send: false, cannot_send_reason: 'dm_limit_reached'
    } })
    api.getConversationMessages.mockResolvedValueOnce({ data: { items: [msg('01', ME)] } })
    const wrapper = await mountChat()
    api.getConversationWith.mockResolvedValueOnce({ data: {
      conversation: conversation(reason === 'recipient_not_found' ? { peer: null } : {}),
      can_send: allowed, cannot_send_reason: reason
    } })
    api.getConversationMessages.mockResolvedValueOnce({ data: { items: [msg('02', PEER, '首次回复'), msg('01', ME)] } })
    notice.dm.value++
    await flushPromises()
    expect(api.getConversationWith).toHaveBeenCalledTimes(2)
    expect(bubbles(wrapper)).toEqual(['m01', '首次回复'])
    expect(wrapper.get('textarea').element.disabled).toBe(!allowed)
    if (reason === 'dm_closed') expect(wrapper.get('.composer-hint').text()).toBe('对方已关闭私信')
    if (reason === 'recipient_not_found') expect(wrapper.find('.peer-link.deleted').exists()).toBe(true)
    wrapper.unmount()
  })

  it('未读数相同仍恢复另一端发言；超过一页的断层使用后端新游标，旧翻页迟到不会跳页', async () => {
    const items = (end, start) => Array.from({ length: end - start + 1 }, (_, index) =>
      msg(String(end - index).padStart(2, '0'), ME))
    api.getConversationWith.mockResolvedValue({ data: { conversation: conversation(), can_send: true } })
    let finishOld
    api.getConversationMessages
      .mockResolvedValueOnce({ data: { items: items(21, 2), has_more: true, next_cursor: 'before-2' } })
      .mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
      .mockResolvedValueOnce({ data: { items: items(42, 23), has_more: true, next_cursor: 'before-23' } })
      .mockResolvedValueOnce({ data: { items: items(22, 3), has_more: true, next_cursor: 'before-3' } })
      .mockResolvedValueOnce({ data: { items: items(2, 1), has_more: false } })
    const wrapper = await mountChat()
    await wrapper.get('.older-btn').trigger('click')
    notice.sync.value++
    await flushPromises()
    finishOld({ data: { items: items(1, 1), has_more: false } })
    await flushPromises()
    expect(bubbles(wrapper)).toEqual(items(42, 23).reverse().map(m => m.content))
    await wrapper.get('.older-btn').trigger('click')
    await flushPromises()
    await wrapper.get('.older-btn').trigger('click')
    await flushPromises()
    expect(bubbles(wrapper)).toEqual(items(42, 1).reverse().map(m => m.content))
    expect(api.getConversationMessages.mock.calls.slice(-2).map(([, page]) => page.cursor)).toEqual(['before-23', 'before-3'])
    wrapper.unmount()
  })

  it('首屏会话尚未返回时的重连与轮询只拉一次历史，重复消息只标读一次', async () => {
    let resolve
    api.getConversationWith.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const wrapper = await mountChat()
    notice.sync.value++
    notice.dm.value++
    await flushPromises()
    resolve({ data: { conversation: conversation(), can_send: true } })
    await flushPromises()
    expect(api.getConversationMessages).toHaveBeenCalledTimes(1)
    expect(api.getConversationWith).toHaveBeenCalledTimes(1)
    notice.handler(msg('02', PEER))
    notice.handler(msg('02', PEER))
    await flushPromises()
    expect(api.markConversationRead).toHaveBeenCalledTimes(2)
    expect(bubbles(wrapper)).toEqual(['m02'])
    wrapper.unmount()
  })

  it('另一个设备删除后的新历史只显示删除之后的记录，新的草稿各有一个键', async () => {
    api.getConversationWith.mockResolvedValue({ data: { conversation: conversation(), can_send: true } })
    api.getConversationMessages.mockResolvedValueOnce({ data: { items: [msg('01', PEER, '历史')] } })
    const wrapper = await mountChat()
    api.getConversationMessages.mockResolvedValueOnce({ data: { items: [msg('02', PEER, '删除后')] } })
    notice.sync.value++
    await flushPromises()
    expect(bubbles(wrapper)).toEqual(['删除后'])
    await typeAndSend(wrapper, 'a')
    await flushPromises()
    await typeAndSend(wrapper, 'b')
    await flushPromises()
    expect(new Set(api.sendMessage.mock.calls.map(([body]) => body.client_msg_id)).size).toBe(2)
    expect(wrapper.find('.composer-hint').exists()).toBe(false)
    wrapper.unmount()
  })

  it('打开自己的聊天页：提示不能给自己发私信', async () => {
    api.getConversationWith = vi.fn(() => Promise.reject(Object.assign(new Error('Cannot have a conversation with yourself'), { code: 201, status: 400 })))
    const wrapper = await mountChat()
    expect(wrapper.find('.composer-hint').text()).toBe('不能给自己发私信')
  })
})
