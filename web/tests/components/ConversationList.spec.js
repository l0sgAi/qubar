import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { RouterView } from 'vue-router'
import { createTestRouter, createTestI18n } from '../helpers/plugins'

const api = vi.hoisted(() => ({
  getConversations: vi.fn(), deleteConversation: vi.fn(), refresh: vi.fn(),
  confirm: vi.fn(), toast: { success: vi.fn(), error: vi.fn() }
}))
const events = vi.hoisted(() => ({}))
vi.mock('@/api/message', () => api)
vi.mock('@/composables/useUnreadNotice', () => ({
  useUnreadNotice({ onDmMessage }) {
    events.receive = onDmMessage
    return { dmUnreadCount: events.unread, dmSyncVersion: events.sync }
  },
  refreshDmUnreadCount: api.refresh
}))
vi.mock('@/components/layout/AppShell.vue', () => ({ default: { render: () => null } }))
vi.mock('@/composables/useAppShell', () => ({
  useAppShell: () => ({ offset: ref(0), mobileShell: ref(false), contentStyle: ref({}) })
}))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()), useDialog: () => ({ warning: api.confirm }), useMessage: () => api.toast
}))

const { default: ConversationList } = await import('@/views/message/ConversationList.vue')
const row = (peer, id, content, unread = 0, sender = peer) => ({
  conversation_id: 'c-' + peer, peer_id: peer, peer: { id: peer, username: peer },
  last_message: { id, content, sender_id: sender, create_time: '2026-11-24T10:00:00Z' },
  unread_count: unread
})
const page = (...items) => ({ data: { items, has_more: false } })
const mountList = async () => {
  const router = createTestRouter([
    { path: '/messages', name: 'messages', component: ConversationList },
    { path: '/messages/:userId', name: 'message-chat' }
  ])
  await router.push('/messages')
  const wrapper = mount(RouterView, { global: { plugins: [router, createTestI18n()] } })
  await flushPromises()
  return { router, wrapper }
}
const previews = wrapper => wrapper.findAll('.conv-preview').map(node => node.text())

describe('ConversationList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    events.sync = ref(0)
    events.unread = ref(0)
  })

  it('首屏旧请求晚回：重连和轮询一起取一份新快照，同总数也呈现另一端发言及已读', async () => {
    let resolve
    api.getConversations.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const { wrapper } = await mountList()
    api.getConversations.mockResolvedValueOnce(page(row('bob', '05', '新消息', 1), row('alice', '04', '另一端已发', 0, 'me')))
    events.sync.value++
    events.unread.value = 1
    await flushPromises()
    resolve(page(row('alice', '01', '旧消息', 1)))
    await flushPromises()
    expect(api.getConversations).toHaveBeenCalledTimes(2)
    expect(previews(wrapper)).toEqual(['新消息', '我：另一端已发'])

    api.getConversations.mockResolvedValueOnce(page(row('alice', '06', '新摘要', 1), row('bob', '05', '新消息')))
    events.sync.value++
    await flushPromises()
    expect(previews(wrapper)).toEqual(['新摘要', '新消息'])
    expect(wrapper.findAll('.conv-item.unread').map(item => item.find('.conv-name').text())).toEqual(['alice'])
    wrapper.unmount()
  })

  it('重复摘要与晚到的旧事件保持顺序；收到截断摘要后，进入聊天再返回取最新列表', async () => {
    api.getConversations.mockResolvedValueOnce(page(row('bob', '05', 'b'), row('alice', '04', 'a')))
    const { wrapper, router } = await mountList()
    const dm = { id: '06', conversation_id: 'c-alice', sender_id: 'alice', content: 'x'.repeat(100), truncated: true }
    events.receive(dm)
    events.receive(dm)
    events.receive({ ...dm, id: '05', conversation_id: 'c-bob', sender_id: 'bob' })
    await flushPromises()
    expect(previews(wrapper)).toEqual([dm.content, 'b'])
    expect(wrapper.findAll('.conv-badge').map(node => node.text())).toEqual(['1'])
    await wrapper.get('.conv-item').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.params.userId).toBe('alice')
    api.getConversations.mockResolvedValueOnce(page(row('alice', '06', dm.content), row('bob', '05', 'b')))
    router.back()
    await flushPromises()
    expect(router.currentRoute.value.name).toBe('messages')
    expect(wrapper.find('.conv-badge').exists()).toBe(false)
    wrapper.unmount()
  })

  it('删除只对本人隐藏；旧响应不得恢复被删项，之后推送使列表按服务端新记录再现', async () => {
    const old = row('alice', '01', '删除前')
    api.getConversations.mockResolvedValueOnce(page(old))
    const { wrapper } = await mountList()
    let resolve
    api.getConversations.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    events.sync.value++
    await flushPromises()
    api.deleteConversation.mockResolvedValueOnce({})
    api.getConversations.mockResolvedValueOnce(page())
    await wrapper.get('.conv-delete').trigger('click')
    await api.confirm.mock.lastCall[0].onPositiveClick()
    await flushPromises()
    resolve(page(old))
    await flushPromises()
    expect(api.deleteConversation).toHaveBeenCalledExactlyOnceWith('c-alice')
    expect(previews(wrapper)).toEqual([])
    expect(api.refresh).toHaveBeenCalledTimes(1)

    api.getConversations.mockResolvedValueOnce(page(row('alice', '02', '删除后', 1)))
    const dm = { id: '02', conversation_id: 'c-alice', sender_id: 'alice' }
    events.receive(dm)
    events.receive(dm)
    await flushPromises()
    expect(previews(wrapper)).toEqual(['删除后'])
    expect(api.getConversations).toHaveBeenCalledTimes(4)
    wrapper.unmount()
  })
})
