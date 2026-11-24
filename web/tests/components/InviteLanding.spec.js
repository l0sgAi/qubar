import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, computed } from 'vue'

const api = vi.hoisted(() => ({ getInvitePreview: null, acceptInvite: null }))
const msg = vi.hoisted(() => ({ success: null, error: null, warning: null, info: null }))
const authState = vi.hoisted(() => ({ loggedIn: true }))

vi.mock('@/api/circleInvite', () => ({
  getInvitePreview: (...a) => api.getInvitePreview(...a),
  acceptInvite: (...a) => api.acceptInvite(...a)
}))
vi.mock('@/utils/auth', () => ({ auth: { isAuthenticated: () => authState.loggedIn } }))
vi.mock('@/composables/useSideNavCircles', () => ({ MEMBERSHIP_CHANGED_EVENT: 'test:membership' }))
vi.mock('@/composables/usePageTitle', () => ({ usePageTitle: () => ({ setTitleData: () => {} }) }))
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
  })
}))

const { default: InviteLanding } = await import('@/views/circle/InviteLanding.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const TOKEN = 'q3vAbc-_0123456789abcdefghijklmnopqrstuvwxy'
const fail = (fields) => Promise.reject(Object.assign(new Error('backend english message'), fields))

let wrapper
let router

const mountPage = async () => {
  router = createTestRouter([
    { path: '/', name: 'login' },
    { path: '/invite/:token' },
    { path: '/circle/:id' },
    { path: '/discover' }
  ])
  await router.push(`/invite/${TOKEN}`)
  wrapper = mount(InviteLanding, {
    attachTo: document.body,
    global: { plugins: [router, createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

const joinButton = () => wrapper.find('.join-btn')

beforeEach(() => {
  authState.loggedIn = true
  api.getInvitePreview = vi.fn(() => Promise.resolve({ data: { name: 'Golang 私密小组', avatar_url: '', member_count: 12 } }))
  api.acceptInvite = vi.fn(() => Promise.resolve({ data: { circle_id: 'c1', already_member: false } }))
  Object.keys(msg).forEach((k) => { msg[k] = vi.fn() })
})

afterEach(() => {
  wrapper?.unmount()
})

describe('InviteLanding 预览', () => {
  it('用路由 token 拉预览，展示圈名和成员数', async () => {
    await mountPage()
    expect(api.getInvitePreview).toHaveBeenCalledWith(TOKEN)
    expect(wrapper.text()).toContain('Golang 私密小组')
    expect(wrapper.text()).toContain('12 位成员')
    expect(joinButton().text()).toBe('加入圈子')
  })

  it('404 统一显示「邀请已失效」，不展示后端原因', async () => {
    api.getInvitePreview = vi.fn(() => fail({ code: 204, status: 404 }))
    await mountPage()
    expect(wrapper.text()).toContain('邀请已失效')
    expect(wrapper.text()).not.toContain('backend english message')
    expect(joinButton().exists()).toBe(false)
  })

  it('其他错误显示可重试', async () => {
    api.getInvitePreview = vi.fn(() => Promise.reject(new Error('Network Error')))
    await mountPage()
    expect(wrapper.text()).toContain('邀请信息加载失败')
    api.getInvitePreview = vi.fn(() => Promise.resolve({ data: { name: '重试后', member_count: 1 } }))
    await wrapper.findAll('button').find(b => b.text() === '重试').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('重试后')
  })
})

describe('InviteLanding 加入', () => {
  it('未登录：跳登录页并带 redirect 回到落地页，不调接口', async () => {
    authState.loggedIn = false
    await mountPage()
    expect(joinButton().text()).toBe('登录后加入')
    await joinButton().trigger('click')
    await flushPromises()
    expect(api.acceptInvite).not.toHaveBeenCalled()
    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.query).toEqual({ redirect: `/invite/${TOKEN}`, tab: 'login' })
  })

  it('加入成功：广播成员变更并跳到圈子页', async () => {
    const listener = vi.fn()
    window.addEventListener('test:membership', listener)
    await mountPage()
    await joinButton().trigger('click')
    await flushPromises()
    expect(api.acceptInvite).toHaveBeenCalledWith({ token: TOKEN })
    expect(msg.success).toHaveBeenCalledWith('已加入「Golang 私密小组」')
    expect(listener).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.fullPath).toBe('/circle/c1')
    window.removeEventListener('test:membership', listener)
  })

  it('already_member=true 同样跳转', async () => {
    api.acceptInvite = vi.fn(() => Promise.resolve({ data: { circle_id: 'c9', already_member: true } }))
    await mountPage()
    await joinButton().trigger('click')
    await flushPromises()
    expect(msg.info).toHaveBeenCalledWith('你已经是该圈子成员')
    expect(router.currentRoute.value.fullPath).toBe('/circle/c9')
  })

  it('403 被拉黑：提示且留在本页', async () => {
    api.acceptInvite = vi.fn(() => fail({ code: 203, status: 403 }))
    await mountPage()
    await joinButton().trigger('click')
    await flushPromises()
    expect(msg.error).toHaveBeenCalledWith('你已被该圈子拉黑，无法加入')
    expect(router.currentRoute.value.fullPath).toBe(`/invite/${TOKEN}`)
  })

  it('加入时 404：切到失效态', async () => {
    api.acceptInvite = vi.fn(() => fail({ code: 204, status: 404 }))
    await mountPage()
    await joinButton().trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('邀请已失效')
    expect(joinButton().exists()).toBe(false)
  })
})
