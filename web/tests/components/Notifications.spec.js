import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref } from 'vue'

const api = vi.hoisted(() => ({ getNoticeList: null, getPostDetail: null, getManagedCircles: null, markNoticesRead: null }))
const msg = vi.hoisted(() => ({ warning: null }))
vi.mock('@/api/notice', () => ({
  getNoticeList: (...args) => api.getNoticeList(...args),
  markNoticesRead: (...args) => api.markNoticesRead(...args),
  markAllNoticesRead: () => Promise.resolve()
}))
vi.mock('@/api/post', () => ({ getPostDetail: (...args) => api.getPostDetail(...args) }))
vi.mock('@/api/circle', () => ({ getManagedCircles: (...args) => api.getManagedCircles(...args) }))
vi.mock('@/composables/useAppShell', () => ({
  useAppShell: () => ({ offset: ref(0), contentStyle: {} })
}))
vi.mock('@/components/layout/AppShell.vue', () => ({ default: { render: () => null } }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({ error: vi.fn(), success: vi.fn(), warning: (...a) => msg.warning(...a) })
}))

const { default: Notifications } = await import('@/views/Notifications.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const NOTICES = [
  { id: 'n1', notice_type: 7, post_id: 'p1', snippet: '过审帖', is_read: false },
  { id: 'n2', notice_type: 8, post_id: 'p2', snippet: '驳回帖', is_read: true },
  { id: 'n3', notice_type: 1, post_id: 'p3', snippet: '被赞帖', is_read: true }
]

let router
const mountPage = async () => {
  router = createTestRouter([
    { path: '/notifications' }, { path: '/post/:id' }, { path: '/profile' }, { path: '/circle/managed' }, { path: '/circle/:id/moderation' }
  ])
  await router.push('/notifications')
  const wrapper = mount(Notifications, { global: { plugins: [router, createTestI18n()] } })
  await flushPromises()
  return wrapper
}

describe('Notifications', () => {
  beforeEach(() => {
    vi.stubGlobal('IntersectionObserver', class { observe() {} disconnect() {} })
    api.getNoticeList = vi.fn(() => Promise.resolve({ data: { notices: NOTICES, cursor: '' } }))
    api.markNoticesRead = vi.fn(() => Promise.resolve())
    api.getPostDetail = vi.fn(() => Promise.resolve({ data: { id: 'p9', circle_id: 'c9' } }))
    api.getManagedCircles = vi.fn(() => Promise.resolve({ data: [{ id: 'c1' }, { id: 'c2' }], total: 2 }))
    msg.warning = vi.fn()
    vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  const REPORTED = { id: 'n9', notice_type: 9, post_id: 'p9', snippet: '被举报帖', is_read: false }
  const mountWith = (notices) => {
    api.getNoticeList = vi.fn(() => Promise.resolve({ data: { notices, cursor: '' } }))
    return mountPage()
  }

  it('7/8 渲染审核文案、系统图标头像；7 链接到帖子详情，8 链接到「我的帖子」', async () => {
    const wrapper = await mountPage()
    const items = wrapper.findAll('.notice-item')
    expect(items.map(el => el.find('.notice-text').text())).toEqual([
      '你的帖子《过审帖》已通过审核',
      '你的帖子《驳回帖》未通过审核',
      '有人 赞了你的帖子《被赞帖》'
    ])
    expect(items.map(el => el.attributes('href'))).toEqual(['/post/p1', '/profile?tab=posts', '/post/p3'])
    // 系统通知用图标头像，普通通知（actor 缺省）仍是首字母兜底
    expect(items[0].find('.system-avatar').exists()).toBe(true)
    expect(items[1].find('.system-avatar').exists()).toBe(true)
    expect(items[2].find('.system-avatar').exists()).toBe(false)
    expect(items[2].find('.notice-avatar').text()).toBe('有')
  })

  it('类型 12 缺 actor 时保留通知文案，隐藏回关与主页链接', async () => {
    const wrapper = await mountWith([{ id: 'n12', notice_type: 12, is_read: true }])
    const item = wrapper.get('.notice-item')
    expect(item.get('.notice-text').text()).toBe('有人关注了你')
    expect(item.attributes('href')).toBeUndefined()
    expect(item.find('.notice-follow-btn').exists()).toBe(false)
  })

  it('「审核」tab 请求数字类型 7,8,9,10,11', async () => {
    const wrapper = await mountPage()
    const reviewTab = wrapper.findAll('.notice-tab').find(el => el.text() === '审核')
    await reviewTab.trigger('click')
    await flushPromises()
    expect(api.getNoticeList).toHaveBeenLastCalledWith({ type: '7,8,9,10,11', size: 20 })
  })

  it('9 带 circle_id：系统图标，直接链接到审核页被举报 tab 并定位', async () => {
    const wrapper = await mountWith([{ ...REPORTED, circle_id: 'c9' }])
    const item = wrapper.find('.notice-item')
    expect(item.find('.notice-text').text()).toBe('本圈帖子《被举报帖》被举报，请及时复核')
    expect(item.find('.system-avatar').exists()).toBe(true)
    expect(item.attributes('href')).toBe('/circle/c9/moderation?tab=reported&focus=p9')
  })

  it('10 带作者头像（非系统图标），链接到帖子详情', async () => {
    const wrapper = await mountWith([{ id: 'n10', notice_type: 10, post_id: 'p10', snippet: 'T', actor: { username: 'Bob' } }])
    const item = wrapper.find('.notice-item')
    expect(item.find('.system-avatar').exists()).toBe(false)
    expect(item.find('.notice-avatar').text()).toBe('B')
    expect(item.attributes('href')).toBe('/post/p10')
  })

  it('9 缺 circle_id：点击标已读，查帖子详情拿圈子后跳审核页定位', async () => {
    const wrapper = await mountWith([REPORTED])
    const item = wrapper.find('.notice-item')
    expect(item.attributes('href')).toBeUndefined()
    expect(item.attributes('tabindex')).toBe('0')
    await item.trigger('click')
    await flushPromises()
    expect(api.markNoticesRead).toHaveBeenCalledWith(['n9'])
    expect(api.getPostDetail).toHaveBeenCalledWith('p9')
    expect(router.currentRoute.value.fullPath).toBe('/circle/c9/moderation?tab=reported&focus=p9')
    expect(msg.warning).not.toHaveBeenCalled()
  })

  it('9 缺 circle_id 且帖子不可见：提示后只管一个圈子时进它的被举报列表', async () => {
    api.getPostDetail = vi.fn(() => Promise.reject(Object.assign(new Error('x'), { code: 204, status: 404 })))
    api.getManagedCircles = vi.fn(() => Promise.resolve({ data: [{ id: 'c1' }], total: 1 }))
    const wrapper = await mountWith([REPORTED])
    await wrapper.find('.notice-item').trigger('click')
    await flushPromises()
    expect(msg.warning).toHaveBeenCalledWith('该帖子已被处理或不可见')
    expect(router.currentRoute.value.fullPath).toBe('/circle/c1/moderation?tab=reported')
  })

  it('9 缺 circle_id 且帖子不可见：管理多个圈子时进「我管理的圈子」', async () => {
    api.getPostDetail = vi.fn(() => Promise.reject(new Error('x')))
    const wrapper = await mountWith([REPORTED])
    await wrapper.find('.notice-item').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(msg.warning).toHaveBeenCalledWith('该帖子已被处理或不可见')
    expect(router.currentRoute.value.fullPath).toBe('/circle/managed')
  })
})
