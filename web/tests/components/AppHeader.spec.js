import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { auth } from '@/utils/auth'

const unread = vi.hoisted(() => ({ count: null, dm: null }))
const req = vi.hoisted(() => ({ get: null }))
const msg = vi.hoisted(() => ({ warning: null, success: null, error: null, info: null }))
const logout = vi.hoisted(() => ({ confirm: null }))

vi.mock('@/composables/useUnreadNotice', () => ({
  useUnreadNotice: () => ({ unreadCount: unread.count, dmUnreadCount: unread.dm })
}))
vi.mock('@/composables/useLogout', () => ({
  useLogout: () => ({ confirmLogout: (...a) => logout.confirm(...a) })
}))
vi.mock('@/utils/request', () => ({
  default: { get: (...a) => req.get(...a) }
}))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({
    warning: (...a) => msg.warning(...a),
    success: (...a) => msg.success(...a),
    error: (...a) => msg.error(...a),
    info: (...a) => msg.info(...a)
  })
}))

const { default: AppHeader } = await import('@/components/layout/AppHeader.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const ROUTES = [
  { path: '/', name: 'login' },
  { path: '/home', name: 'home' },
  { path: '/discover', name: 'discover' },
  { path: '/messages', name: 'messages' },
  { path: '/messages/:userId', name: 'message-chat' },
  { path: '/notifications', name: 'notifications' },
  { path: '/profile', name: 'profile' },
  { path: '/create-post', name: 'create-post' },
  { path: '/search', name: 'search' }
]

let wrapper
let router

const loginAs = () => {
  auth.setToken('tok-1')
  localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
}

const mountHeader = async (path = '/home') => {
  router = createTestRouter(ROUTES)
  await router.push(path)
  wrapper = mount(AppHeader, {
    attachTo: document.body,
    global: { plugins: [router, createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

// 展开头像下拉菜单（AppDropdown 默认 trigger="hover"，NDropdown 传送到 document.body）
const openAvatarMenu = async () => {
  await wrapper.find('.user-info-trigger').trigger('mouseenter')
  vi.advanceTimersByTime(300)
  await nextTick()
  await flushPromises()
}

const getMenuOptions = () => Array.from(document.body.querySelectorAll('.n-dropdown-option'))
const findMessagesOption = () => getMenuOptions().find(el => el.textContent?.includes('私信'))
const hasAvatarDot = () => wrapper.find('.avatar-unread-badge .n-badge-sup').exists()

describe('AppHeader', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    unread.count = ref(0)
    unread.dm = ref(0)
    req.get = vi.fn(() => Promise.resolve({ data: { username: 'Alice', avatar_url: '' } }))
    logout.confirm = vi.fn()
    Object.keys(msg).forEach((k) => { msg[k] = vi.fn() })
    loginAs()
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
    document.body.innerHTML = ''
  })

  it('顶栏移除独立私信图标，仅保留发帖、消息中心铃铛与头像菜单', async () => {
    await mountHeader()
    expect(wrapper.find('[aria-label="私信"]').exists()).toBe(false)
    expect(wrapper.findAll('.notification-wrapper')).toHaveLength(1)
    expect(wrapper.find('.user-info-trigger').exists()).toBe(true)
  })

  it('同一已挂载组件中私信未读 0 → 2 → 0：红点与菜单角标同步，展开菜单不清除未读，归零后入口仍可用', async () => {
    await mountHeader()

    // 初始 0：头像无红点，菜单有「私信」但无未读数字
    expect(hasAvatarDot()).toBe(false)
    await openAvatarMenu()
    let dmOption = findMessagesOption()
    expect(dmOption).toBeTruthy()
    expect(dmOption.querySelector('.user-menu-badge')).toBeNull()

    // 0 → 2：头像右上角显示红点，菜单项显示未读总数 2
    unread.dm.value = 2
    await nextTick()
    expect(hasAvatarDot()).toBe(true)

    // 收起后重新展开菜单，确认仅展开菜单不会触发已读或清除 2
    await wrapper.find('.user-info-trigger').trigger('mouseleave')
    vi.advanceTimersByTime(300)
    await nextTick()
    const callsBeforeOpen = req.get.mock.calls.length
    await openAvatarMenu()
    expect(unread.dm.value).toBe(2)
    expect(hasAvatarDot()).toBe(true)
    dmOption = findMessagesOption()
    expect(dmOption).toBeTruthy()
    expect(dmOption.querySelector('.user-menu-badge')?.textContent?.trim()).toBe('2')
    expect(req.get.mock.calls.length).toBe(callsBeforeOpen)

    // 2 → 0：头像红点和菜单未读数同步消失，「私信」入口仍可点击跳转 /messages
    unread.dm.value = 0
    await nextTick()
    expect(hasAvatarDot()).toBe(false)
    dmOption = findMessagesOption()
    expect(dmOption).toBeTruthy()
    expect(dmOption.querySelector('.user-menu-badge')).toBeNull()

    dmOption.querySelector('.n-dropdown-option-body').click()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/messages')
  })

  it('私信未读为 0、铃铛通知大于 0 时，铃铛显示通知数且头像没有红点', async () => {
    unread.count.value = 5
    unread.dm.value = 0
    await mountHeader()

    // 消息中心铃铛显示自己的通知计数
    const bellBadge = wrapper.find('.notification-badge')
    expect(bellBadge.exists()).toBe(true)
    expect(bellBadge.text()).toContain('5')

    // 头像没有红点，展开后「私信」项也没有角标
    expect(hasAvatarDot()).toBe(false)
    await openAvatarMenu()
    const dmOption = findMessagesOption()
    expect(dmOption).toBeTruthy()
    expect(dmOption.querySelector('.user-menu-badge')).toBeNull()
  })

  it('已有会话深链页面中头像菜单仍可展示并保留跳转能力', async () => {
    unread.dm.value = 120
    await mountHeader('/messages/u-target')

    expect(router.currentRoute.value.fullPath).toBe('/messages/u-target')
    expect(hasAvatarDot()).toBe(true)
    await openAvatarMenu()
    const dmOption = findMessagesOption()
    expect(dmOption.querySelector('.user-menu-badge')?.textContent?.trim()).toBe('99+')

    dmOption.querySelector('.n-dropdown-option-body').click()
    await flushPromises()
    expect(router.currentRoute.value.fullPath).toBe('/messages')
  })
})
