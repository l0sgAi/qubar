import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'

const unread = vi.hoisted(() => ({ count: null, dm: null }))
vi.mock('@/composables/useUnreadNotice', () => ({
  useUnreadNotice: () => ({ unreadCount: unread.count, dmUnreadCount: unread.dm })
}))
const invites = vi.hoisted(() => ({ count: null, refresh: null }))
vi.mock('@/composables/usePendingInvites', () => ({
  usePendingInvites: () => ({ pendingCount: invites.count, refreshPendingInvites: (...a) => invites.refresh(...a) })
}))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({ warning: vi.fn() })
}))

const { default: MobileTabBar } = await import('@/components/layout/mobile/MobileTabBar.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const ROUTES = ['/', '/home', '/discover', '/hot', '/messages', '/notifications', '/profile', '/post/:id'].map(path => ({ path }))

const mountAt = async (path) => {
  const router = createTestRouter(ROUTES)
  await router.push(path)
  return mount(MobileTabBar, { global: { plugins: [router, createTestI18n()] } })
}

const labels = (wrapper) => wrapper.findAll('.tab-label').map(el => el.text())
const active = (wrapper) => wrapper.findAll('.tab-item.active').map(el => el.find('.tab-label').text())

describe('MobileTabBar', () => {
  beforeEach(() => {
    unread.count = ref(0)
    unread.dm = ref(0)
    invites.count = ref(0)
    invites.refresh = vi.fn()
  })

  it('「我的」显示待接受的圈子邀请数，挂载时刷新', async () => {
    localStorage.setItem('qubar_token', 'tok')
    localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
    invites.count = ref(3)
    const wrapper = await mountAt('/home')
    expect(invites.refresh).toHaveBeenCalled()
    const me = wrapper.findAll('.tab-item').at(4)
    expect(me.find('.badge').text()).toBe('3')
    expect(wrapper.findAll('.badge')).toHaveLength(1)
  })

  it('访客：首页 / 发现 / 登录，登录项带 redirect', async () => {
    const wrapper = await mountAt('/post/1')
    expect(labels(wrapper)).toEqual(['首页', '发现', '登录'])
    const loginHref = wrapper.findAll('a').at(2).attributes('href')
    expect(loginHref).toBe('/?redirect=/post/1&tab=login')
  })

  it('登录态：首页 / 发现 / 私信 / 消息 / 我的', async () => {
    localStorage.setItem('qubar_token', 'tok')
    localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
    const wrapper = await mountAt('/home')
    expect(labels(wrapper)).toEqual(['首页', '发现', '私信', '消息', '我的'])
  })

  it('私信角标用私信未读数，与消息中心角标分开', async () => {
    localStorage.setItem('qubar_token', 'tok')
    localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
    unread.count.value = 2
    unread.dm.value = 7
    const wrapper = await mountAt('/messages')
    const tabs = wrapper.findAll('.tab-item')
    expect(tabs.at(2).find('.badge').text()).toBe('7')
    expect(tabs.at(3).find('.badge').text()).toBe('2')
    expect(active(wrapper)).toEqual(['私信'])
    unread.dm.value = 0
    await wrapper.vm.$nextTick()
    expect(tabs.at(2).find('.badge').exists()).toBe(false)
  })

  it.each([
    ['/home', ['首页']],
    ['/hot', ['发现']],
    ['/post/1', []]
  ])('%s 高亮 %o', async (path, expected) => {
    const wrapper = await mountAt(path)
    expect(active(wrapper)).toEqual(expected)
  })

  it('未读角标：0 不显示，超过 99 显示 99+', async () => {
    localStorage.setItem('qubar_token', 'tok')
    localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
    const wrapper = await mountAt('/home')
    expect(wrapper.find('.badge').exists()).toBe(false)
    unread.count.value = 5
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.badge').text()).toBe('5')
    unread.count.value = 120
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.badge').text()).toBe('99+')
  })
})
