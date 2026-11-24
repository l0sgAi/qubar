import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import MyPosts from '@/components/user/MyPosts.vue'
import MyFavorites from '@/components/user/MyFavorites.vue'
import BrowseHistory from '@/components/user/BrowseHistory.vue'
import RightSidebar from '@/components/layout/RightSidebar.vue'
import Hot from '@/views/feed/Hot.vue'
import PostDetail from '@/views/post/PostDetail.vue'
import { __resetPostShare } from '@/composables/usePostShare'
import { __resetBreakpoint } from '@/composables/useBreakpoint'
import { auth } from '@/utils/auth'
import { createTestI18n, createTestRouter } from '../helpers/plugins'
import { setMatchMedia } from '../setup'

const api = vi.hoisted(() => ({
  getMyPosts: vi.fn(), getUserPosts: vi.fn(), getHomeFeed: vi.fn(), getPostDetail: vi.fn(),
  sharePost: vi.fn(), getCollectedPosts: vi.fn(), getHistoryPosts: vi.fn(), getTrending: vi.fn()
}))
vi.mock('@/api/post', () => api)
vi.mock('@/api/collect', () => ({ getCollectedPosts: api.getCollectedPosts, toggleCollect: vi.fn() }))
vi.mock('@/api/history', () => ({ getHistoryPosts: api.getHistoryPosts }))
vi.mock('@/api/trending', () => ({ getTrending: api.getTrending }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()), useMessage: () => ({ error: vi.fn(), success: vi.fn() })
}))

const stubs = {
  AppShell: true, MobileFeedSegment: true, AppDropdown: true,
  PostMoreMenu: true, CommentList: true, CommentEditor: true, MobileComposeBar: true
}
const routes = [{ path: '/' }, { path: '/post/:id', name: 'post-detail' }, { path: '/user/:id' }, { path: '/circle/:id' }]
let wrapper

const render = async (component, props = {}) => {
  const router = createTestRouter(routes)
  await router.push('/')
  wrapper = mount(component, { props, global: { plugins: [createTestI18n(), router], stubs } })
  await flushPromises()
  return router
}

beforeEach(() => {
  __resetPostShare()
  __resetBreakpoint()
  Object.values(api).forEach(fn => fn.mockReset())
})
afterEach(() => wrapper?.unmount())

const lists = [
  ['我的帖子', MyPosts, api.getMyPosts, {}],
  ['他人主页', MyPosts, api.getUserPosts, { userId: 'other', readonly: true }],
  ['收藏', MyFavorites, api.getCollectedPosts, {}],
  ['浏览历史', BrowseHistory, api.getHistoryPosts, {}]
]

describe.each([[false, '桌面'], [true, '移动']])('列表请求到分享按钮（mobile=%s）', (mobile) => {
  it.each(lists)('%s 显示实数、兼容旧响应，未发布帖子不显示分享', async (_name, page, fetch, props) => {
    setMatchMedia(mobile)
    const published = { id: 'p', status: 1, title: '帖子', share_count: 0 }
    fetch.mockResolvedValue({ data: { posts: [
      published,
      { ...published, id: 'legacy', share_count: undefined },
      ...[0, 2, 3, 4].map(status => ({ ...published, status, id: String(status) }))
    ] } })
    const writeText = vi.fn().mockResolvedValue()
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText }, share: undefined })
    const router = await render(page, props)
    const actions = wrapper.findAll(mobile ? '.m-share .share-stat' : '.post-stats .share-stat')
    expect(actions.map(button => button.text())).toEqual(['0', ''])
    await actions[0].trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(window.location.origin + '/post/p')
    expect(router.currentRoute.value.path).toBe('/')
    expect(api.getPostDetail).not.toHaveBeenCalled()
    expect(api.sharePost).not.toHaveBeenCalled()
  })
})

it('侧栏最新帖子使用首页返回的数量、个人状态；只读计数位于详情链接内', async () => {
  auth.setToken('tok-sidebar', 60)
  api.getHomeFeed.mockResolvedValue({ data: { posts: [
    { id: 'p', share_count: 0, is_shared: true }, { id: 'old' }
  ] } })
  await render(RightSidebar)
  expect(api.getHomeFeed).toHaveBeenCalledWith(expect.objectContaining({ tab: 'latest' }))
  const cards = wrapper.findAll('.recent-post-card')
  expect(cards[0].attributes('href')).toBe('/post/p')
  expect(cards[0].find('.share-count.is-shared').text()).toBe('0')
  expect(cards[1].find('.share-count').exists()).toBe(false)
  expect(wrapper.find('button.share-stat').exists()).toBe(false)
  expect(api.getPostDetail).not.toHaveBeenCalled()
})

it('热点聚合仅在帖子行展示分享；圈子、用户仍分别跳转', async () => {
  const data = { id: 'p', share_count: 8, is_shared: true, hot_score: 36 }
  api.getTrending.mockResolvedValue({ data: { posts: [data], circles: [data], users: [data] } })
  await render(Hot)
  expect(wrapper.findAll('.share-count')).toHaveLength(1)
  expect(wrapper.find('.type-post .share-count.is-shared').text()).toBe('8')
  for (const type of ['post', 'circle', 'user']) {
    expect(wrapper.find('.type-' + type).attributes('href')).toBe('/' + type + '/p')
    expect(wrapper.find('.type-' + type + ' .hot-score').text()).toBe('36')
  }
  expect(api.getPostDetail).not.toHaveBeenCalled()
})

it.each([[Hot, api.getTrending], [RightSidebar, api.getHomeFeed]])('服务端失败和成功空列表分开展示，重试后复原', async (page, fetch) => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  if (page === RightSidebar) auth.setToken('tok-sidebar', 60)
  fetch.mockRejectedValueOnce(new Error('500')).mockResolvedValueOnce({ data: { posts: [] } })
  await render(page)
  const error = wrapper.get('[role="alert"]')
  expect(error.text()).toContain('失败')
  await error.get('button').trigger('click')
  await flushPromises()
  expect(fetch).toHaveBeenCalledTimes(2)
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(wrapper.find(page === Hot ? '.hot-empty' : '.empty-text').text()).toBeTruthy()
})

it('个人帖子搜索失败可以重试，只有成功的空列表提示发帖', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  api.getMyPosts.mockRejectedValueOnce(new Error('500')).mockResolvedValueOnce({ data: { posts: [] } })
  await render(MyPosts)
  expect(wrapper.find('.empty-hint').exists()).toBe(false)
  await wrapper.get('[role="alert"] button').trigger('click')
  await flushPromises()
  expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  expect(wrapper.find('.empty-hint').exists()).toBe(true)
})

it('列表进入详情分享再返回，旧列表计数与个人高亮均更新', async () => {
  auth.setToken('tok-p', 60)
  api.getMyPosts.mockResolvedValue({ data: { posts: [{ id: 'p', status: 1, title: '分享', share_count: 0 }] } })
  api.getPostDetail.mockResolvedValue({ data: { id: 'p', status: 1, content: '', title: '分享', share_count: 0 } })
  api.sharePost.mockResolvedValue({ data: { first_share: true, share_count: 1 } })
  vi.stubGlobal('navigator', { ...navigator, share: undefined, clipboard: { writeText: vi.fn().mockResolvedValue() } })
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: MyPosts }, { path: '/post/:id', name: 'post-detail', component: PostDetail }
  ] })
  await router.push('/')
  wrapper = mount(RouterView, { global: { plugins: [router, createTestI18n()], stubs } })
  await flushPromises()
  await wrapper.get('.post-cover-link').trigger('click')
  await flushPromises()
  expect(router.currentRoute.value.path).toBe('/post/p')
  await wrapper.get('.share-action').trigger('click')
  await flushPromises()
  expect(api.sharePost).toHaveBeenCalledWith('p', 'tok-p')
  router.back()
  await flushPromises()
  expect(wrapper.get('.is-shared .share-stat').text()).toBe('1')
})

it('访客打开私密圈帖子外链，详情 404 只呈现不可用提示', async () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  api.getPostDetail.mockRejectedValue(Object.assign(new Error('Post not found'), { status: 404, code: 204 }))
  const router = createTestRouter(routes)
  await router.push('/post/private')
  wrapper = mount(PostDetail, { global: { plugins: [router, createTestI18n()], stubs } })
  await flushPromises()
  expect(api.getPostDetail).toHaveBeenCalledWith('private')
  expect(wrapper.find('.n-empty').exists()).toBe(true)
  expect(wrapper.find('.post-detail-container').exists()).toBe(false)
  expect(wrapper.find('.share-action').exists()).toBe(false)
  expect(api.sharePost).not.toHaveBeenCalled()
})
