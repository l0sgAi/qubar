import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { h } from 'vue'
import { NTag } from 'naive-ui'

const mocks = vi.hoisted(() => ({ getUserInfo: vi.fn(), requireLogin: vi.fn() }))

vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({ info: vi.fn() })
}))
vi.mock('@/api/auth', () => ({ getUserInfo: mocks.getUserInfo }))
vi.mock('@/utils/guest-action', () => ({ requireLogin: mocks.requireLogin }))
import MobilePostItem from '@/components/post/MobilePostItem.vue'
import PostCard from '@/components/post/PostCard.vue'
import PostStatusTag from '@/components/post/PostStatusTag.vue'
import PostMoreMenu from '@/components/post/PostMoreMenu.vue'
import ReportPostModal from '@/components/post/ReportPostModal.vue'
import AppDropdown from '@/components/common/AppDropdown.vue'
import { markPostReported, __resetPostReport } from '@/composables/usePostReport'
import { __resetBreakpoint } from '@/composables/useBreakpoint'
import { setMatchMedia } from '../setup'
import { createTestI18n, createTestRouter } from '../helpers/plugins'

const routes = [{ path: '/' }, { path: '/post/:id' }, { path: '/circle/:id' }, { path: '/user/:id' }]

const plugins = () => [createTestI18n(), createTestRouter(routes)]

const mountItem = (props = {}, slots = {}) => mount(MobilePostItem, {
  props: { postId: 'p1', title: '标题', primaryName: '圈子A', ...props },
  slots,
  global: { plugins: plugins() }
})

describe('MobilePostItem', () => {
  it('整项链接到帖子详情，头部显示主 / 次来源与时间', () => {
    const wrapper = mountItem({
      content: '摘要',
      primaryTo: '/circle/c1',
      secondaryName: '作者B',
      secondaryTo: '/user/u1',
      timeText: '3 小时前'
    })
    expect(wrapper.find('.m-cover-link').attributes('href')).toBe('/post/p1')
    expect(wrapper.find('.m-source').attributes('href')).toBe('/circle/c1')
    expect(wrapper.find('.m-source-name').text()).toBe('圈子A')
    expect(wrapper.find('.m-secondary').attributes('href')).toBe('/user/u1')
    expect(wrapper.find('.m-time').text()).toBe('3 小时前')
    expect(wrapper.find('.m-summary').text()).toBe('摘要')
  })

  it('无链接的来源渲染为普通元素（点击落到封面链接）', () => {
    const wrapper = mountItem({ secondaryName: '圈子C' })
    expect(wrapper.find('.m-source').element.tagName).toBe('DIV')
    expect(wrapper.find('.m-secondary').element.tagName).toBe('SPAN')
  })

  it('无图不渲染缩略图；单图无计数；多图显示张数', async () => {
    const wrapper = mountItem()
    expect(wrapper.find('.m-thumb').exists()).toBe(false)

    await wrapper.setProps({ images: ['a.jpg'] })
    expect(wrapper.find('.m-thumb img').attributes('src')).toBe('a.jpg')
    expect(wrapper.find('.m-thumb-count').exists()).toBe(false)

    await wrapper.setProps({ images: ['a.jpg', 'b.jpg', 'c.jpg'] })
    expect(wrapper.find('.m-thumb-count').text()).toBe('3')
  })

  it('置顶 / 精华徽标、meta 插槽、通栏样式', () => {
    const wrapper = mountItem({ pinned: true, essence: true, flush: true }, { meta: '<span class="extra">审核中</span>' })
    expect(wrapper.find('.m-badge.is-pinned').exists()).toBe(true)
    expect(wrapper.find('.m-badge.is-essence').exists()).toBe(true)
    expect(wrapper.find('.m-meta-extra .extra').text()).toBe('审核中')
    expect(wrapper.classes()).toContain('flush')
  })

  it('无 meta 插槽时不渲染右侧容器', () => {
    expect(mountItem().find('.m-meta-extra').exists()).toBe(false)
  })
})

describe('MobilePostItem 驳回 / 屏蔽态（我的帖子）', () => {
  beforeEach(() => {
    __resetBreakpoint()
    document.body.innerHTML = ''
  })

  // 与 MyPosts 一致：状态标签放在 #meta 插槽
  const mountWithStatus = (status, tone = '') => mount(MobilePostItem, {
    props: { postId: 'p1', title: '标题', primaryName: '作者', content: '摘要', tone, flush: true },
    slots: { meta: () => h(PostStatusTag, { status }) },
    global: { plugins: plugins() },
    attachTo: document.body
  })

  const popoverText = () => document.body.querySelector('.post-status-hint')?.textContent || ''

  it('无 tone：不加色条 / 变淡样式', () => {
    const wrapper = mountItem()
    expect(wrapper.classes().some(c => c.startsWith('is-tone-'))).toBe(false)
  })

  it('驳回（status=3）：「未通过审核」+ warning 色条，统计照常显示，整项仍链接到详情', () => {
    const wrapper = mountWithStatus(3, 'warning')
    expect(wrapper.classes()).toContain('is-tone-warning')
    const tag = wrapper.find('.m-meta-extra .post-status-tag')
    expect(tag.exists()).toBe(true)
    expect(tag.text()).toBe('未通过审核')
    expect(wrapper.findComponent(NTag).props('type')).toBe('warning')
    expect(tag.attributes('aria-label')).toContain('仅你自己可见')
    expect(wrapper.find('.m-cover-link').attributes('href')).toBe('/post/p1')
    expect(wrapper.findAll('.m-stat')).toHaveLength(3)
    wrapper.unmount()
  })

  it('移动端：点按标签显示说明（不依赖 hover）', async () => {
    setMatchMedia(true)
    const wrapper = mountWithStatus(3, 'warning')
    expect(popoverText()).toBe('')
    await wrapper.find('.post-status-tag').trigger('click')
    await flushPromises()
    expect(popoverText()).toContain('仅你自己可见。帖子未通过 AI 审核')
    wrapper.unmount()
  })

  it('桌面：悬停标签显示说明', async () => {
    vi.useFakeTimers()
    const wrapper = mountWithStatus(4, 'error')
    await wrapper.find('.post-status-tag').trigger('mouseenter')
    await vi.runAllTimersAsync()
    expect(popoverText()).toContain('帖子被多次举报')
    wrapper.unmount()
  })

  it('屏蔽（status=4）：「已屏蔽，等待复核」+ error 色条', () => {
    const wrapper = mountWithStatus(4, 'error')
    expect(wrapper.classes()).toContain('is-tone-error')
    expect(wrapper.find('.post-status-tag').text()).toBe('已屏蔽，等待复核')
    expect(wrapper.findComponent(NTag).props('type')).toBe('error')
    wrapper.unmount()
  })

  it('审核中（status=2）：说明与 #29 提示条共用「仅你自己可见」', () => {
    const wrapper = mountWithStatus(2)
    expect(wrapper.find('.post-status-tag').text()).toBe('审核中')
    expect(wrapper.find('.post-status-tag').attributes('aria-label')).toContain('帖子正在审核中，仅你自己可见')
    wrapper.unmount()
  })

  it('无说明的状态（草稿）：只渲染普通标签', () => {
    const wrapper = mountWithStatus(0)
    expect(wrapper.find('.post-status-tag').exists()).toBe(false)
    expect(wrapper.find('.m-meta-extra .n-tag').text()).toBe('草稿')
    wrapper.unmount()
  })
})

describe('PostCard 移动端切换', () => {
  beforeEach(() => {
    __resetBreakpoint()
  })

  const props = {
    postId: 'p1',
    circleId: 'c1',
    circleName: '圈子A',
    userId: 'u1',
    userName: '作者B',
    title: '标题',
    postTime: Date.now(),
    coverImage: 'cover.jpg'
  }
  const mountCard = (extra = {}) => mount(PostCard, {
    props: { ...props, ...extra },
    global: { plugins: plugins() }
  })

  it('桌面：渲染原卡片', () => {
    const wrapper = mountCard()
    expect(wrapper.find('.post-card').exists()).toBe(true)
    expect(wrapper.find('.m-post-item').exists()).toBe(false)
  })

  it('移动端：渲染紧凑列表项，主来源为圈子、次来源为作者，封面图作缩略图', () => {
    setMatchMedia(true)
    const wrapper = mountCard()
    expect(wrapper.find('.post-card').exists()).toBe(false)
    expect(wrapper.find('.m-source').attributes('href')).toBe('/circle/c1')
    expect(wrapper.find('.m-secondary').attributes('href')).toBe('/user/u1')
    expect(wrapper.find('.m-thumb img').attributes('src')).toBe('cover.jpg')
  })

  it('移动端圈子内（showCircle=false）：主来源为作者，无次来源', () => {
    setMatchMedia(true)
    const wrapper = mountCard({ showCircle: false })
    expect(wrapper.find('.m-source').attributes('href')).toBe('/user/u1')
    expect(wrapper.find('.m-source-name').text()).toBe('作者B')
    expect(wrapper.find('.m-secondary').exists()).toBe(false)
  })

  it('跨断点切换：不 remount，直接换渲染分支', async () => {
    const wrapper = mountCard()
    setMatchMedia(true)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.m-post-item').exists()).toBe(true)
    setMatchMedia(false)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.post-card').exists()).toBe(true)
  })
})

describe('MobilePostItem 更多菜单（举报）', () => {
  beforeEach(() => {
    __resetPostReport()
    __resetBreakpoint()
    mocks.getUserInfo.mockReset().mockResolvedValue({ data: { id: 'me' } })
    mocks.requireLogin.mockReset()
  })

  const login = () => {
    localStorage.setItem('qubar_token', 'tok')
    localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
  }

  const mountWithMenu = async (props = {}) => {
    const wrapper = mountItem({ moreMenu: true, authorId: 'u2', status: 1, ...props })
    await flushPromises()
    return wrapper
  }

  const menuOptions = (wrapper) => wrapper.findComponent(AppDropdown).props('options')

  it('默认不开（「我的帖子」/ 收藏 / 浏览历史不显示）', () => {
    expect(mountItem({ authorId: 'u2', status: 1 }).find('.post-more-menu').exists()).toBe(false)
  })

  it('访客：已发布的帖子在头部右侧显示「…」，菜单只有「举报」', async () => {
    const wrapper = await mountWithMenu()
    const menu = wrapper.find('.m-meta .post-more-menu')
    expect(menu.exists()).toBe(true)
    expect(menu.classes()).toContain('m-more')
    expect(wrapper.find('.post-more-btn').attributes('aria-label')).toBe('更多操作')
    expect(menuOptions(wrapper)).toEqual([{ key: 'report', label: '举报', disabled: false }])
    expect(mocks.getUserInfo).not.toHaveBeenCalled()
  })

  it('点击菜单不冒泡到列表项（不触发整项跳转）', async () => {
    const wrapper = await mountWithMenu()
    const onItemClick = vi.fn()
    wrapper.element.addEventListener('click', onItemClick)
    await wrapper.find('.post-more-btn').trigger('click')
    expect(onItemClick).not.toHaveBeenCalled()
  })

  it('访客点「举报」：弹登录引导，不打开举报弹窗', async () => {
    const wrapper = await mountWithMenu()
    wrapper.findComponent(AppDropdown).vm.$emit('select', 'report')
    await flushPromises()
    expect(mocks.requireLogin).toHaveBeenCalledTimes(1)
    expect(wrapper.findComponent(ReportPostModal).exists()).toBe(false)
  })

  it('已登录点「举报」：打开该帖子的举报弹窗', async () => {
    login()
    const wrapper = await mountWithMenu()
    wrapper.findComponent(AppDropdown).vm.$emit('select', 'report')
    await flushPromises()
    const modal = wrapper.findComponent(ReportPostModal)
    expect(modal.exists()).toBe(true)
    expect(modal.props()).toMatchObject({ show: true, postId: 'p1' })
    expect(mocks.requireLogin).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('自己的帖子不显示；别人的帖子显示', async () => {
    login()
    expect((await mountWithMenu({ authorId: 'me' })).find('.post-more-menu').exists()).toBe(false)
    expect((await mountWithMenu({ authorId: 'u2' })).find('.post-more-menu').exists()).toBe(true)
    expect(mocks.getUserInfo).toHaveBeenCalledTimes(1)
  })

  it('未发布的帖子不显示', async () => {
    for (const status of [2, 3, 4]) {
      expect((await mountWithMenu({ status })).find('.post-more-menu').exists()).toBe(false)
    }
  })

  it('本次会话已举报过：菜单项置灰为「已举报」', async () => {
    login()
    markPostReported('p1')
    const wrapper = await mountWithMenu()
    expect(menuOptions(wrapper)).toEqual([{ key: 'report', label: '已举报', disabled: true }])
  })

  it('PostCard：移动端列表项与桌面卡片都带菜单，作者取 userId，状态默认已发布', async () => {
    const props = { postId: 'p1', circleId: 'c1', circleName: '圈子A', userId: 'u1', userName: '作者B', title: '标题', postTime: Date.now() }
    const desktop = mount(PostCard, { props, global: { plugins: plugins() } })
    expect(desktop.find('.post-card .post-header .post-more-menu').exists()).toBe(true)
    expect(desktop.findComponent(PostMoreMenu).props()).toMatchObject({ postId: 'p1', authorId: 'u1', status: 1 })

    setMatchMedia(true)
    const mobile = mount(PostCard, { props, global: { plugins: plugins() } })
    expect(mobile.find('.m-post-item .m-meta .post-more-menu').exists()).toBe(true)
    expect(mobile.findComponent(MobilePostItem).props()).toMatchObject({ moreMenu: true, authorId: 'u1', status: 1 })
  })
})
