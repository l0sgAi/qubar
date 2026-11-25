import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { NLayoutSider, NMenu } from 'naive-ui'

const circles = vi.hoisted(() => ({
  ensure: null,
  refresh: null
}))
const invites = vi.hoisted(() => ({ count: null, refresh: null }))

vi.mock('@/composables/useSideNavCircles', () => ({
  useSideNavCircles: () => ({
    joinedCircles: ref([{ id: 'c1', name: '测试圈', avatar: '' }]),
    activeCircles: ref([]),
    randomCircles: ref([]),
    circlesLoading: ref(false),
    activeCirclesLoading: ref(false),
    randomCirclesLoading: ref(false),
    ensureSideNavData: (...a) => circles.ensure(...a),
    refreshJoinedCircles: (...a) => circles.refresh(...a)
  })
}))
vi.mock('@/composables/usePendingInvites', () => ({
  usePendingInvites: () => ({
    pendingCount: invites.count,
    refreshPendingInvites: (...a) => invites.refresh(...a)
  })
}))
vi.mock('@/components/circle/CreateCircleModal.vue', () => ({
  default: { name: 'CreateCircleModal', render: () => null }
}))

const { default: SideNav } = await import('@/components/layout/SideNav.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const ROUTES = [
  { path: '/home', name: 'home' },
  { path: '/hot', name: 'hot' },
  { path: '/discover', name: 'discover' },
  { path: '/messages', name: 'messages' },
  { path: '/messages/:userId', name: 'message-chat' },
  { path: '/invites', name: 'my-invites' },
  { path: '/admin/agents', name: 'admin-agents' },
  { path: '/circle/:id', name: 'circle-detail' },
  { path: '/profile', name: 'profile' }
]

let wrapper
let router

const mountNav = async (path = '/home') => {
  router = createTestRouter(ROUTES)
  await router.push(path)
  wrapper = mount(SideNav, {
    attachTo: document.body,
    global: { plugins: [router, createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

const optionKeys = () => {
  const menu = wrapper.findComponent(NMenu)
  return (menu.props('options') || []).map(opt => opt.key)
}

describe('SideNav', () => {
  beforeEach(() => {
    circles.ensure = vi.fn()
    circles.refresh = vi.fn()
    invites.count = ref(0)
    invites.refresh = vi.fn()
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    document.body.innerHTML = ''
  })

  it('展开和收起状态均不包含「私信」入口', async () => {
    await mountNav('/home')

    // 展开状态
    expect(optionKeys()).not.toContain('messages')
    expect(wrapper.text()).not.toContain('私信')
    expect(wrapper.find('a[href="/messages"]').exists()).toBe(false)

    // 切换到收起状态
    const sider = wrapper.findComponent(NLayoutSider)
    sider.vm.$emit('collapse')
    await nextTick()

    expect(wrapper.findComponent(NMenu).props('collapsed')).toBe(true)
    expect(optionKeys()).not.toContain('messages')
    expect(wrapper.find('a[href="/messages"]').exists()).toBe(false)
    expect(optionKeys()).toEqual(expect.arrayContaining(['home', 'hot', 'explore', 'admin-agents', 'create']))
  })

  it('访问私信路由或会话深链时正常渲染且不选中任何私信菜单项', async () => {
    await mountNav('/messages/u-42')
    const menu = wrapper.findComponent(NMenu)
    expect(menu.props('value')).toBeNull()
    expect(optionKeys()).not.toContain('messages')
  })

  it('保留待接受圈子邀请入口', async () => {
    invites.count.value = 2
    await mountNav('/home')
    expect(optionKeys()).toContain('my-invites')
    expect(wrapper.find('a[href="/invites"]').exists()).toBe(true)
  })
})
