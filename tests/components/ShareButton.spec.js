import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  sharePost: vi.fn(),
  getUserInfo: vi.fn(),
  message: { success: vi.fn(), error: vi.fn(), warning: vi.fn(), info: vi.fn() }
}))

vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => mocks.message
}))
vi.mock('@/api/post', () => ({ sharePost: mocks.sharePost }))
vi.mock('@/api/auth', () => ({ getUserInfo: mocks.getUserInfo }))

import ShareButton from '@/components/post/ShareButton.vue'
import MobilePostItem from '@/components/post/MobilePostItem.vue'
import PostCard from '@/components/post/PostCard.vue'
import DiscoverPostTile from '@/components/feed/DiscoverPostTile.vue'
import PostHeaderAndContent from '@/components/post/detail/PostHeaderAndContent.vue'
import { __resetPostShare, applyShareResult } from '@/composables/usePostShare'
import { createTestI18n, createTestRouter } from '../helpers/plugins'

const routes = [{ path: '/' }, { path: '/post/:id' }, { path: '/circle/:id' }, { path: '/user/:id' }]
const global = () => ({ plugins: [createTestI18n(), createTestRouter(routes)] })

const login = (token = 'tok-a') => {
  localStorage.setItem('qubar_token', token)
  localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
}

const stubClipboard = () => {
  const writeText = vi.fn().mockResolvedValue()
  vi.stubGlobal('navigator', { ...navigator, share: undefined, clipboard: { writeText } })
  return writeText
}

const mountButton = (props = {}) => mount(ShareButton, {
  props: { postId: 'p1', title: 'T', ...props },
  global: global()
})

describe('ShareButton', () => {
  beforeEach(() => {
    __resetPostShare()
    mocks.sharePost.mockReset()
    mocks.getUserInfo.mockReset().mockResolvedValue({ data: { id: 'me' } })
    Object.values(mocks.message).forEach(fn => fn.mockReset())
  })

  it('显示分享数；没有分享数只显示图标', () => {
    expect(mountButton({ shareCount: 12 }).find('.share-stat').text()).toBe('12')
    const bare = mountButton()
    expect(bare.find('.share-stat').text()).toBe('')
    expect(bare.find('.share-stat').attributes('aria-label')).toBe('分享')
  })

  it('访客：复制链接并提示，不调接口', async () => {
    const writeText = stubClipboard()
    const wrapper = mountButton({ shareCount: 1 })
    await wrapper.find('.share-stat').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/post/p1`)
    expect(mocks.message.success).toHaveBeenCalledWith('链接已复制')
    expect(mocks.sharePost).not.toHaveBeenCalled()
    expect(wrapper.find('.share-wrap').classes()).not.toContain('is-shared')
  })

  it('登录用户首次分享：分享数覆盖、高亮并播放 +1', async () => {
    login()
    stubClipboard()
    mocks.sharePost.mockResolvedValue({ data: { share_count: 4, first_share: true } })
    const wrapper = mountButton({ shareCount: 3 })
    await wrapper.find('.share-stat').trigger('click')
    await flushPromises()
    expect(wrapper.find('.share-stat').text()).toBe('4')
    expect(wrapper.find('.share-wrap').classes()).toContain('is-shared')
    expect(wrapper.find('.share-bump').exists()).toBe(true)
    await wrapper.find('.share-bump').trigger('animationend')
    expect(wrapper.find('.share-bump').exists()).toBe(false)
  })

  it('再次分享：数字不变，不播放动效', async () => {
    login()
    stubClipboard()
    mocks.sharePost.mockResolvedValue({ data: { share_count: 4, first_share: false } })
    const wrapper = mountButton({ shareCount: 4, isShared: true })
    await wrapper.find('.share-stat').trigger('click')
    await flushPromises()
    expect(wrapper.find('.share-stat').text()).toBe('4')
    expect(wrapper.find('.share-bump').exists()).toBe(false)
  })

  it('404：链接照常复制，另提示内容不可分享', async () => {
    login()
    stubClipboard()
    mocks.sharePost.mockRejectedValue(Object.assign(new Error('Post not found'), { status: 404, code: 204 }))
    const wrapper = mountButton()
    await wrapper.find('.share-stat').trigger('click')
    await flushPromises()
    expect(mocks.message.success).toHaveBeenCalledWith('链接已复制')
    expect(mocks.message.warning).toHaveBeenCalledWith('内容不可分享')
  })

  it('复制失败提示', async () => {
    vi.stubGlobal('navigator', { ...navigator, share: undefined, clipboard: { writeText: vi.fn().mockRejectedValue(new Error('x')) } })
    document.execCommand = vi.fn().mockReturnValue(false)
    const wrapper = mountButton()
    await wrapper.find('.share-stat').trigger('click')
    await flushPromises()
    expect(mocks.message.error).toHaveBeenCalledWith('复制失败，请手动复制链接')
  })

  it('button 变体：未分享 / 已分享文案', async () => {
    const wrapper = mountButton({ variant: 'button' })
    expect(wrapper.find('.share-action').text()).toBe('分享')
    await wrapper.setProps({ isShared: true })
    expect(wrapper.find('.share-action').text()).toBe('已分享')
  })

  it('列表与详情共用分享结果', () => {
    login()
    const card = mountButton({ shareCount: 1 })
    const detail = mountButton({ variant: 'button', shareCount: 1 })
    applyShareResult('p1', { share_count: 2, first_share: true })
    return flushPromises().then(() => {
      expect(card.find('.share-stat').text()).toBe('2')
      expect(detail.find('.share-action').text()).toBe('已分享')
    })
  })
})

describe('分享入口在各卡片上的显示', () => {
  beforeEach(() => {
    __resetPostShare()
    mocks.getUserInfo.mockReset().mockResolvedValue({ data: { id: 'me' } })
  })

  it('MobilePostItem：shareable 开启且已发布才显示', () => {
    const base = { postId: 'p1', title: 'T' }
    expect(mount(MobilePostItem, { props: base, global: global() }).findComponent(ShareButton).exists()).toBe(false)
    const on = mount(MobilePostItem, { props: { ...base, shareable: true, shareCount: 2 }, global: global() })
    expect(on.find('.m-share .share-stat').text()).toBe('2')
    const draft = mount(MobilePostItem, { props: { ...base, shareable: true, status: 0 }, global: global() })
    expect(draft.findComponent(ShareButton).exists()).toBe(false)
  })

  it('PostCard：桌面统计栏带分享；未发布不显示', () => {
    const props = { postId: 'p1', circleName: 'C', userName: 'U', title: 'T', postTime: Date.now(), shareCount: 5, isShared: true }
    const card = mount(PostCard, { props, global: global() })
    const btn = card.findComponent(ShareButton)
    expect(btn.props()).toMatchObject({ postId: 'p1', shareCount: 5, isShared: true, variant: 'stat' })
    expect(mount(PostCard, { props: { ...props, status: 0 }, global: global() }).findComponent(ShareButton).exists()).toBe(false)
  })

  it('DiscoverPostTile：只读分享数，已分享高亮', () => {
    const tile = mount(DiscoverPostTile, {
      props: { post: { postId: 'p1', title: 'T', shareCount: 7, isShared: true } },
      global: global()
    })
    const stat = tile.find('.stat.is-shared')
    expect(stat.text()).toBe('7')
    const noCount = mount(DiscoverPostTile, { props: { post: { postId: 'p1', title: 'T' } }, global: global() })
    expect(noCount.findAll('.stat')).toHaveLength(3)
  })

  it('详情：统计栏显示分享数，操作栏有分享按钮', () => {
    const post = { id: 'p1', title: 'T', content: '', status: 1, share_count: 3, is_shared: false, view_count: 0, comment_count: 0, like_count: 0, collect_count: 0 }
    const wrapper = mount(PostHeaderAndContent, {
      props: { post },
      global: { ...global(), stubs: { MentionPreview: true, PostMoreMenu: true, PostStatusBanner: true } }
    })
    expect(wrapper.findAll('.post-stats .stat-item').at(-1).text()).toBe('3')
    expect(wrapper.findComponent(ShareButton).props()).toMatchObject({ postId: 'p1', shareCount: 3, isShared: false, variant: 'button' })
  })
})
