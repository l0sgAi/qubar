import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { shallowMount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import AdminAgents from '@/views/admin/AdminAgents.vue'
import CircleAgents from '@/views/circle/CircleAgents.vue'
import { createTestI18n, createTestRouter } from '../helpers/plugins'

const api = vi.hoisted(() => ({
  getUserInfo: vi.fn(), getCircleDetail: vi.fn(), getAgentList: vi.fn(), getCircleAgentList: vi.fn()
}))
vi.mock('@/api/agent', () => ({ getAgentList: api.getAgentList }))
vi.mock('@/api/circle', () => ({ getCircleDetail: api.getCircleDetail, getCircleAgentList: api.getCircleAgentList }))
vi.mock('@/api/auth', () => ({ getUserInfo: api.getUserInfo }))
vi.mock('@/composables/usePageTitle', () => ({ usePageTitle: () => ({ setTitleData: vi.fn() }) }))
vi.mock('naive-ui', async original => ({ ...(await original()), useMessage: () => ({ error: vi.fn() }) }))

const rows = [
  { id: 'manual', status: 1, trigger_mode: 3 }, { id: 'off', status: 0, trigger_mode: 3 },
  { id: 'keyword', status: 1, trigger_mode: 2 }
]
const DataTable = defineComponent({
  props: ['columns', 'data'],
  setup(props) {
    return () => h('section', props.data.map(row =>
      h('div', { 'data-row': row.id }, props.columns.find(col => col.key === 'actions').render(row))))
  }
})
let wrapper
const open = async (component, path) => {
  const router = createTestRouter([
    { path: '/admin/agents', name: 'admin-agents' }, { path: '/circle/:id/agents', name: 'circle-agents' }
  ])
  await router.push(path)
  wrapper = shallowMount(component, {
    global: {
      renderStubDefaultSlot: true, plugins: [router, createTestI18n()],
      stubs: { DataTable, Button: { template: '<button><slot /></button>' } }
    }
  })
  await flushPromises()
  return router
}
const replies = () => wrapper.findAll('button').filter(b => b.text() === '手动回复')

describe('机器人管理页面手动回复入口', () => {
  beforeEach(() => {
    vi.resetAllMocks()
    api.getAgentList.mockResolvedValue({ data: rows })
    api.getCircleAgentList.mockResolvedValue({ data: rows })
  })
  afterEach(() => wrapper?.unmount())

  it.each([1, 0])('全局角色 %i 只对平台管理员展示启用的手动机器人', async role => {
    api.getUserInfo.mockResolvedValue({ data: { role } })
    await open(AdminAgents, '/admin/agents')
    expect(replies()).toHaveLength(role === 1 ? 1 : 0)
    if (role === 1) {
      await replies()[0].trigger('click')
      expect(wrapper.findComponent({ name: 'AgentReplyModal' }).props('agent').id).toBe('manual')
    }
  })

  it('圈主有入口；切到另一个圈后关闭弹窗，普通圈管理员没有入口', async () => {
    api.getCircleDetail.mockResolvedValueOnce({ data: { member_role: 30 } }).mockResolvedValueOnce({ data: { member_role: 20 } })
    const router = await open(CircleAgents, '/circle/owner/agents')
    expect(replies()).toHaveLength(1)
    await replies()[0].trigger('click')
    expect(wrapper.findComponent({ name: 'AgentReplyModal' }).props('circle')).toBe(true)
    await router.push('/circle/admin/agents')
    await flushPromises()
    expect(wrapper.findComponent({ name: 'AgentReplyModal' }).exists()).toBe(false)
    expect(replies()).toHaveLength(0)
    expect(wrapper.get('[data-row="manual"]').text()).toContain('编辑')
  })
})
