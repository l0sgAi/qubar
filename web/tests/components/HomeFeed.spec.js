import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { getHomeFeed } from '@/api/post'
import Home from '@/views/feed/Home.vue'
import { auth } from '@/utils/auth'
import { createTestI18n, createTestRouter } from '../helpers/plugins'

vi.mock('@/api/post', () => ({ getHomeFeed: vi.fn() }))
vi.mock('naive-ui', async (original) => ({
  ...(await original()), useMessage: () => ({ error: vi.fn(), info: vi.fn() })
}))

let page
let scroll
async function openFeed() {
  const router = createTestRouter([{ path: '/', name: 'home' }])
  await router.push('/')
  page = mount(Home, { global: { plugins: [createTestI18n(), router], stubs: {
    AppShell: true, RightSidebar: true, PostList: true, PostListSkeleton: true
  } } })
  await flushPromises()
}

beforeEach(() => {
  getHomeFeed.mockReset()
  vi.spyOn(console, 'error').mockImplementation(() => {})
  vi.stubGlobal('IntersectionObserver', class {
    constructor(cb) { this.cb = cb }
    observe() { scroll = () => this.cb([{ isIntersecting: true }]) }
    disconnect() { scroll = undefined }
  })
})
afterEach(() => page?.unmount())

describe('首页接口失败与分页', () => {
  it('访客 hot 首屏失败可重试，成功空响应才显示空态', async () => {
    getHomeFeed.mockRejectedValueOnce(new Error('500')).mockResolvedValueOnce({ data: { posts: [] } })
    await openFeed()
    const alert = page.get('[role="alert"]')
    expect(getHomeFeed).toHaveBeenCalledWith({ tab: 'hot', size: 20 })
    await alert.get('button').trigger('click')
    await flushPromises()
    expect(page.find('[role="alert"]').exists()).toBe(false)
    expect(page.get('.feed-empty').text()).toBe(createTestI18n().global.t('feed.empty'))
  })

  it('推荐池和关注游标独立，翻页失败保留已加载列表与游标，手动重试一次', async () => {
    auth.setToken('local-user', 60)
    const cursor = '["2026-11-01","p"]'
    getHomeFeed.mockResolvedValueOnce({ data: { posts: [{ id: 'a' }], pool_token: 'pool', has_more: true } })
      .mockResolvedValueOnce({ data: { posts: [{ id: 'b' }] } })
      .mockResolvedValueOnce({ data: { posts: [{ id: 'c' }], search_after: cursor, has_more: true } })
      .mockRejectedValueOnce(new Error('500')).mockResolvedValueOnce({ data: { posts: [{ id: 'd' }] } })
    await openFeed()
    scroll()
    await flushPromises()
    expect(getHomeFeed).toHaveBeenNthCalledWith(2, { tab: 'recommend', size: 20, pool_token: 'pool', offset: 20 })
    await page.getComponent({ name: 'Tabs' }).vm.$emit('update:value', 'following')
    await flushPromises()
    expect(getHomeFeed).toHaveBeenNthCalledWith(3, { tab: 'following', size: 20 })
    scroll()
    await flushPromises()
    expect(scroll).toBeUndefined()
    expect(page.find('post-list-stub').exists()).toBe(true)
    await page.get('[role="alert"] button').trigger('click')
    await flushPromises()
    for (const n of [4, 5]) {
      expect(getHomeFeed).toHaveBeenNthCalledWith(n, { tab: 'following', size: 20, search_after: cursor })
    }
    expect(page.find('[role="alert"]').exists()).toBe(false)
  })
})
