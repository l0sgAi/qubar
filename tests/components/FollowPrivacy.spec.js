import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import FollowList from '@/views/follow/FollowList.vue'
import UserDetail from '@/views/user/UserDetail.vue'
import { followUser, getFollowingList, getFollowStats } from '@/api/follow'
import { getUserDetail } from '@/api/user'
import { requireLogin } from '@/utils/guest-action'
import { auth } from '@/utils/auth'
import { createTestI18n, createTestRouter } from '../helpers/plugins'

vi.mock('@/api/follow', () => ({ followUser: vi.fn(), getFollowingList: vi.fn(), getFollowStats: vi.fn() }))
vi.mock('@/api/user', () => ({ getUserDetail: vi.fn() }))
vi.mock('@/api/auth', () => ({ getUserInfo: () => Promise.resolve({ data: { id: 'viewer' } }) }))
vi.mock('@/utils/guest-action', () => ({ requireLogin: vi.fn() }))
vi.mock('@/composables/usePostReport', () => ({ ensureViewer: () => Promise.resolve('viewer') }))
vi.mock('@/components/layout/AppShell.vue', () => ({ default: { render: () => null } }))
vi.mock('@/composables/useAppShell', () => ({ useAppShell: () => ({ offset: ref(0), contentStyle: {} }) }))
vi.mock('@/composables/usePageTitle', () => ({ usePageTitle: () => ({ setTitleData: vi.fn() }) }))
vi.mock('naive-ui', async (original) => ({ ...(await original()), useMessage: () => ({ error: vi.fn() }) }))

let wrapper
async function open(component, path) {
  const router = createTestRouter([
    { name: 'user-detail', path: '/user/:id' },
    { name: 'user-following', path: '/user/:id/following' },
    { name: 'user-followers', path: '/user/:id/followers' }
  ])
  await router.push(path)
  wrapper = mount(component, {
    global: {
      plugins: [router, createTestI18n()],
      stubs: { MyPosts: true, MyGroups: true }
    }
  })
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  getUserDetail.mockResolvedValue({ data: { id: 'owner', username: 'Owner', role: 0 } })
  getFollowingList.mockResolvedValue({ data: { items: [{ user: { id: 'peer', username: 'Peer' } }], has_more: false } })
})
afterEach(() => wrapper.unmount())

describe('关注隐私与主页入口', () => {
  it.each([
    ['本人', 'owner-session', true],
    ['他人', 'other-session', false],
    ['访客', null, false]
  ])('私密列表的%s视角，按服务器 list_visible 决定是否请求明细', async (_, token, visible) => {
    if (token) auth.setToken(token)
    getFollowStats.mockResolvedValue({ data: { following_count: 9, follower_count: 1, list_visible: visible } })
    await open(FollowList, '/user/owner/following')
    expect(getFollowStats).toHaveBeenCalledWith('owner')
    expect(wrapper.findAll('.tab-count').map(el => el.text())).toEqual(['9', '1'])
    expect(getFollowingList).toHaveBeenCalledTimes(visible ? 1 : 0)
    if (visible) expect(wrapper.text()).toContain('Peer')
    else expect(wrapper.text()).toContain('对方未公开关注和粉丝列表')
  })

  it.each([false, true])('AI 主页提供关注入口并隐藏私信；已登录=%s', async loggedIn => {
    if (loggedIn) auth.setToken('viewer-session')
    getUserDetail.mockResolvedValue({ data: { id: 'bot', username: 'Bot', role: 2 } })
    getFollowStats.mockResolvedValue({ data: { list_visible: true, relation: 0 } })
    followUser.mockResolvedValue({ data: { relation: 1 } })
    await open(UserDetail, '/user/bot')
    expect(wrapper.find('.dm-button').exists()).toBe(false)
    await wrapper.get('button.follow-button').trigger('click')
    await flushPromises()
    if (loggedIn) expect(followUser).toHaveBeenCalledWith('bot')
    else {
      expect(requireLogin).toHaveBeenCalledWith('follow')
      expect(followUser).not.toHaveBeenCalled()
    }
  })
})
