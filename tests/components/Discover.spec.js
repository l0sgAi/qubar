import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, computed } from 'vue'

const api = vi.hoisted(() => ({ getDiscover: null }))
const io = vi.hoisted(() => ({ callbacks: [] }))

vi.mock('@/api/discover', () => ({ getDiscover: (...a) => api.getDiscover(...a) }))
vi.mock('@/utils/auth', () => ({ auth: { isAuthenticated: () => false } }))
vi.mock('@/composables/useAppShell', () => ({
  useAppShell: () => ({ offset: ref(260), mobileShell: computed(() => false), contentStyle: computed(() => ({})) })
}))
vi.mock('@/components/layout/AppShell.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/layout/mobile/MobileFeedSegment.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/layout/RightSidebar.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/post/PostCard.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/post/PostList.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/feed/DiscoverCircleCard.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/feed/DiscoverPostTile.vue', () => ({ default: { render: () => null } }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({ success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() })
}))

const { default: Discover } = await import('@/views/feed/Discover.vue')
const { createTestI18n } = await import('../helpers/plugins')

const post = (id) => ({ id, circle_id: 'c', title: id })
const circle = (id) => ({ id, name: id })
const posts = (n, prefix = 'p') => Array.from({ length: n }, (_, i) => post(`${prefix}${i}`))

let wrapper

const mountPage = async () => {
  wrapper = mount(Discover, { global: { plugins: [createTestI18n()] } })
  await flushPromises()
}

// 触发当前所有哨兵 observer（模拟滚到底部）
const scrollToBottom = async () => {
  io.callbacks.forEach(cb => cb([{ isIntersecting: true }]))
  await flushPromises()
}

describe('Discover：私密圈帖子被过滤后的分页（后端 #170）', () => {
  beforeEach(() => {
    io.callbacks = []
    vi.stubGlobal('IntersectionObserver', class {
      constructor(cb) { this.cb = cb }
      observe() { io.callbacks.push(this.cb) }
      disconnect() { io.callbacks = io.callbacks.filter(c => c !== this.cb) }
    })
  })

  afterEach(() => {
    wrapper?.unmount()
  })

  it('首屏失败时显示错误，刷新成功才展示空态', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    api.getDiscover = vi.fn().mockRejectedValueOnce(new Error('500')).mockResolvedValueOnce({ data: {} })
    await mountPage()
    expect(wrapper.get('[role="alert"]').text()).toBe(createTestI18n().global.t('discover.loadFailed'))
    await wrapper.get('.refresh-btn').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('.discover-empty').text()).toBe(createTestI18n().global.t('discover.empty'))
  })

  it('首屏帖子不足 size 条时仍可继续加载；offset 按池切片（size）累加，而不是已展示条数', async () => {
    api.getDiscover = vi.fn((params) => {
      if (params.section === 'all') {
        // 20 条池切片里有 17 条私密圈帖子被过滤，只剩 3 条
        return Promise.resolve({ data: { circles: [circle('c1')], posts: posts(3), pool_token: 'tk', has_more: true } })
      }
      return Promise.resolve({ data: { posts: posts(2, `o${params.offset}-`), pool_token: 'tk', has_more: true } })
    })
    await mountPage()

    expect(wrapper.text()).not.toContain('没有更多了')

    await scrollToBottom()
    expect(api.getDiscover).toHaveBeenLastCalledWith(expect.objectContaining({ section: 'posts', offset: 20, pool_token: 'tk' }))

    await scrollToBottom()
    expect(api.getDiscover).toHaveBeenLastCalledWith(expect.objectContaining({ section: 'posts', offset: 40 }))
  })

  it('单分区 has_more=false 后停止加载帖子', async () => {
    api.getDiscover = vi.fn((params) => {
      if (params.section === 'all') {
        return Promise.resolve({ data: { circles: [], posts: posts(5), pool_token: 'tk', has_more: false } })
      }
      return Promise.resolve({ data: { posts: [], pool_token: 'tk', has_more: false } })
    })
    await mountPage()

    await scrollToBottom()
    const calls = api.getDiscover.mock.calls.length
    await scrollToBottom()
    expect(api.getDiscover.mock.calls.length).toBe(calls)
  })

  it('池已重建（pool_refreshed）：offset 重置后从 size 起算', async () => {
    api.getDiscover = vi.fn((params) => {
      if (params.section === 'all') {
        return Promise.resolve({ data: { circles: [], posts: posts(20), pool_token: 'old', has_more: true } })
      }
      if (params.pool_token === 'old') {
        return Promise.resolve({ data: { posts: posts(4, 'n'), pool_token: 'new', has_more: true, pool_refreshed: true } })
      }
      return Promise.resolve({ data: { posts: posts(4, 'm'), pool_token: 'new', has_more: true } })
    })
    await mountPage()

    await scrollToBottom()
    await scrollToBottom()
    expect(api.getDiscover).toHaveBeenLastCalledWith(expect.objectContaining({ section: 'posts', offset: 20, pool_token: 'new' }))
  })
})
