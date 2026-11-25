import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, computed } from 'vue'
import { NEmpty, NIcon } from 'naive-ui'
import { InboxOutlined } from '@vicons/antd'
import { auth } from '@/utils/auth'
import { __resetPendingInvites, usePendingInvites } from '@/composables/usePendingInvites'

const api = vi.hoisted(() => ({ getMyInvites: null, respondEmailInvite: null }))
const msg = vi.hoisted(() => ({ success: null, error: null, warning: null, info: null }))
const dialogs = vi.hoisted(() => [])
vi.mock('@/api/circleInvite', () => ({
  getMyInvites: (...a) => api.getMyInvites(...a),
  respondEmailInvite: (...a) => api.respondEmailInvite(...a)
}))
vi.mock('@/composables/useSideNavCircles', () => ({ MEMBERSHIP_CHANGED_EVENT: 'test:membership' }))
vi.mock('@/composables/useAppShell', () => ({
  useAppShell: () => ({ offset: ref(260), mobileShell: computed(() => false), contentStyle: computed(() => ({})) })
}))
vi.mock('@/components/layout/AppShell.vue', () => ({ default: { render: () => null } }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({
    success: (...a) => msg.success(...a),
    error: (...a) => msg.error(...a),
    warning: (...a) => msg.warning(...a),
    info: (...a) => msg.info(...a)
  }),
  useDialog: () => ({ warning: (opts) => dialogs.push(opts) })
}))

const { default: MyInvites } = await import('@/views/circle/MyInvites.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const deferred = () => {
  let resolve
  const promise = new Promise(done => { resolve = done })
  return { promise, resolve }
}

const fail = (fields) => Promise.reject(Object.assign(new Error('backend english message'), fields))
const invite = (extra = {}) => ({
  invite_id: 'i1',
  circle_id: 'c1',
  circle_name: 'Golang 私密小组',
  circle_avatar: '',
  inviter_name: '圈主小王',
  create_time: '2026-10-30T12:00:00+08:00',
  ...extra
})

let wrapper
let router

const mountPage = async () => {
  router = createTestRouter([{ path: '/invites' }, { path: '/circle/:id' }])
  await router.push('/invites')
  wrapper = mount(MyInvites, {
    attachTo: document.body,
    global: { plugins: [router, createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

const btn = (item, text) => item.findAll('button').find(b => b.text() === text)

beforeEach(() => {
  dialogs.length = 0
  api.getMyInvites = vi.fn(() => Promise.resolve({ data: [invite(), invite({ invite_id: 'i2', circle_id: 'c2', circle_name: '另一个圈' })], total: 2 }))
  api.respondEmailInvite = vi.fn(() => Promise.resolve({ data: { circle_id: 'c1', already_member: false } }))
  __resetPendingInvites()
  auth.setToken('session-a')
  Object.keys(msg).forEach((k) => { msg[k] = vi.fn() })
})

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

describe('MyInvites', () => {
  it('展示待接受邀请并同步角标总数', async () => {
    await mountPage()
    expect(api.getMyInvites).toHaveBeenCalledWith({ page: 1, size: 20 })
    const items = wrapper.findAll('.invite-item')
    expect(items).toHaveLength(2)
    expect(items[0].text()).toContain('Golang 私密小组')
    expect(items[0].text()).toContain('圈主小王 邀请你加入')
    expect(usePendingInvites().pendingCount.value).toBe(2)
  })

  it('空列表显示空态并使用 NIcon 渲染 InboxOutlined 图标', async () => {
    api.getMyInvites = vi.fn(() => Promise.resolve({ data: [], total: 0 }))
    await mountPage()
    const empty = wrapper.findComponent(NEmpty)
    expect(empty.exists()).toBe(true)
    expect(wrapper.text()).toContain('暂无待接受的圈子邀请')
    const icon = empty.findComponent(NIcon)
    expect(icon.exists()).toBe(true)
    expect(icon.findComponent(InboxOutlined).exists()).toBe(true)
    expect(usePendingInvites().pendingCount.value).toBe(0)
  })

  it('接受：广播成员变更、刷新角标并跳到圈子页', async () => {
    const listener = vi.fn()
    window.addEventListener('test:membership', listener)
    await mountPage()
    await btn(wrapper.findAll('.invite-item')[0], '接受').trigger('click')
    await flushPromises()
    expect(api.respondEmailInvite).toHaveBeenCalledWith({ invite_id: 'i1', accept: true })
    expect(msg.success).toHaveBeenCalledWith('已加入「Golang 私密小组」')
    expect(listener).toHaveBeenCalledTimes(1)
    expect(api.getMyInvites).toHaveBeenCalledWith({ page: 1, size: 1 })
    expect(router.currentRoute.value.fullPath).toBe('/circle/c1')
    window.removeEventListener('test:membership', listener)
  })

  it('接受返回 already_member 同样跳转', async () => {
    api.respondEmailInvite = vi.fn(() => Promise.resolve({ data: { circle_id: 'c1', already_member: true } }))
    await mountPage()
    await btn(wrapper.findAll('.invite-item')[0], '接受').trigger('click')
    await flushPromises()
    expect(msg.info).toHaveBeenCalledWith('你已经是该圈子成员')
    expect(router.currentRoute.value.fullPath).toBe('/circle/c1')
  })

  it('接受返回 404：提示「邀请已失效」并刷新列表', async () => {
    api.respondEmailInvite = vi.fn(() => fail({ code: 204, status: 404 }))
    await mountPage()
    await btn(wrapper.findAll('.invite-item')[0], '接受').trigger('click')
    await flushPromises()
    expect(msg.error).toHaveBeenCalledWith('邀请已失效')
    expect(api.getMyInvites).toHaveBeenCalledTimes(2)
    expect(router.currentRoute.value.fullPath).toBe('/invites')
  })

  it('接受返回 403：提示被拉黑，不刷新', async () => {
    api.respondEmailInvite = vi.fn(() => fail({ code: 203, status: 403 }))
    await mountPage()
    await btn(wrapper.findAll('.invite-item')[0], '接受').trigger('click')
    await flushPromises()
    expect(msg.error).toHaveBeenCalledWith('你已被该圈子拉黑，无法加入')
    expect(api.getMyInvites).toHaveBeenCalledTimes(1)
  })

  it('拒绝需二次确认，确认后 accept=false 并刷新', async () => {
    await mountPage()
    await btn(wrapper.findAll('.invite-item')[1], '拒绝').trigger('click')
    expect(api.respondEmailInvite).not.toHaveBeenCalled()
    expect(dialogs[0].content).toContain('另一个圈')
    await dialogs[0].onPositiveClick()
    await flushPromises()
    expect(api.respondEmailInvite).toHaveBeenCalledWith({ invite_id: 'i2', accept: false })
    expect(msg.success).toHaveBeenCalledWith('已拒绝邀请')
    expect(api.getMyInvites).toHaveBeenCalledTimes(2)
  })

  it('迟到的 A 列表不可覆盖 B 的私密邀请和角标', async () => {
    const oldList = deferred()
    api.getMyInvites.mockReturnValueOnce(oldList.promise)
    await mountPage()
    api.getMyInvites.mockResolvedValue({ data: [invite({ circle_name: 'B 的圈子' })], total: 1 })
    auth.setToken('session-b')
    await flushPromises()
    oldList.resolve({ data: [invite()], total: 9 })
    await flushPromises()
    expect(wrapper.findAll('.invite-item')).toHaveLength(1)
    expect(wrapper.text()).toContain('B 的圈子')
    expect(wrapper.text()).not.toContain('Golang 私密小组')
    expect(usePendingInvites().pendingCount.value).toBe(1)
  })

  it('切换账号后关闭旧列表，旧确认框不能以新账号提交拒绝', async () => {
    await mountPage()
    await btn(wrapper.findAll('.invite-item')[1], '拒绝').trigger('click')
    api.getMyInvites.mockResolvedValue({ data: [], total: 0 })
    auth.setToken('session-b')
    await dialogs[0].onPositiveClick()
    await flushPromises()
    expect(api.respondEmailInvite).not.toHaveBeenCalled()
    expect(wrapper.findAll('.invite-item')).toHaveLength(0)
    expect(usePendingInvites().pendingCount.value).toBe(0)
  })

  it('退出后返回的接受结果不跳转、不广播成员变化、不重新点亮角标', async () => {
    const result = deferred()
    api.respondEmailInvite.mockReturnValueOnce(result.promise)
    await mountPage()
    await btn(wrapper.findAll('.invite-item')[0], '接受').trigger('click')
    auth.clearToken()
    const events = vi.spyOn(window, 'dispatchEvent')
    result.resolve({ data: { circle_id: 'c1' } })
    await flushPromises()
    expect(events.mock.calls.map(([event]) => event.type)).not.toContain('test:membership')
    expect(router.currentRoute.value.fullPath).toBe('/invites')
    expect(wrapper.findAll('.invite-item')).toHaveLength(0)
    expect(usePendingInvites().pendingCount.value).toBe(0)
  })
})
