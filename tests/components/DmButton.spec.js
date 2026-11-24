import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const api = vi.hoisted(() => ({ getUserInfo: null }))
const guest = vi.hoisted(() => ({ requireLogin: null }))
vi.mock('@/api/auth', () => ({ getUserInfo: (...a) => api.getUserInfo(...a) }))
vi.mock('@/utils/guest-action', () => ({ requireLogin: (...a) => guest.requireLogin(...a) }))

const { default: DmButton, __resetDmButtonCache } = await import('@/components/message/DmButton.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const PEER = { id: 'peer-1', role: 0 }

const login = () => {
  localStorage.setItem('qubar_token', 'tok')
  localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
}

let router
const mountButton = async (props, attrs = {}) => {
  router = createTestRouter([{ path: '/' }, { path: '/messages/:userId' }])
  await router.push('/')
  const wrapper = mount(DmButton, { props, attrs, global: { plugins: [router, createTestI18n()] } })
  await flushPromises()
  return wrapper
}

describe('DmButton', () => {
  beforeEach(() => {
    __resetDmButtonCache()
    api.getUserInfo = vi.fn(() => Promise.resolve({ data: { id: 'me-1' } }))
    guest.requireLogin = vi.fn()
  })

  it('单根元素：父组件传入的 class / style 落在按钮上（profile-action 布局依赖它）', async () => {
    login()
    const wrapper = await mountButton({ user: PEER }, { class: 'profile-action', style: 'margin-top: 16px' })
    const btn = wrapper.find('button')
    expect(btn.exists()).toBe(true)
    expect(wrapper.element).toBe(btn.element)
    expect(btn.classes()).toContain('profile-action')
    expect(btn.attributes('style')).toContain('margin-top: 16px')
  })

  it('登录用户点击进入与对方的聊天页', async () => {
    login()
    const wrapper = await mountButton({ user: PEER })
    await wrapper.find('button').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/messages/peer-1')
  })

  it('看自己的主页 / 机器人账号不显示', async () => {
    login()
    const self = await mountButton({ user: { id: 'me-1', role: 0 } })
    expect(self.find('button').exists()).toBe(false)
    const bot = await mountButton({ user: { id: 'bot-1', role: 2 } })
    expect(bot.find('button').exists()).toBe(false)
  })

  it('访客：显示按钮，点击弹登录引导，不请求自己的资料', async () => {
    const wrapper = await mountButton({ user: PEER })
    expect(api.getUserInfo).not.toHaveBeenCalled()
    await wrapper.find('button').trigger('click')
    expect(guest.requireLogin).toHaveBeenCalledWith('message')
    expect(router.currentRoute.value.path).toBe('/')
  })
})
