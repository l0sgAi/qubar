import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, computed } from 'vue'

const api = vi.hoisted(() => ({
  getCircleDetail: null,
  getCirclePosts: null,
  joinCircle: null,
  leaveCircle: null,
  getModerationCounts: null
}))
const msg = vi.hoisted(() => ({ success: null, error: null, warning: null, info: null }))
const authState = vi.hoisted(() => ({ loggedIn: true }))

const shell = vi.hoisted(() => ({ mobile: false }))
const shareOrCopy = vi.hoisted(() => vi.fn())
vi.mock('@/utils/share', async (original) => ({ ...(await original()), shareOrCopy }))
vi.mock('@/api/circle', () => ({
  getCircleDetail: (...a) => api.getCircleDetail(...a),
  getCirclePosts: (...a) => api.getCirclePosts(...a),
  joinCircle: (...a) => api.joinCircle(...a),
  leaveCircle: (...a) => api.leaveCircle(...a)
}))
vi.mock('@/api/moderation', () => ({ getModerationCounts: (...a) => api.getModerationCounts(...a) }))
vi.mock('@/utils/auth', () => ({ auth: { isAuthenticated: () => authState.loggedIn } }))
vi.mock('@/utils/guest-action', () => ({ requireLogin: vi.fn() }))
vi.mock('@/composables/useSideNavCircles', () => ({ MEMBERSHIP_CHANGED_EVENT: 'test:membership' }))
vi.mock('@/composables/usePageTitle', () => ({ usePageTitle: () => ({ setTitleData: () => {} }) }))
vi.mock('@/composables/useAppShell', () => ({
  useAppShell: () => ({ offset: ref(260), mobileShell: computed(() => shell.mobile), contentStyle: computed(() => ({})) })
}))
vi.mock('@/components/layout/AppShell.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/post/PostList.vue', () => ({
  default: { props: ['posts'], template: '<div class="post-list-stub">{{ posts.length }}</div>' }
}))
vi.mock('@/components/post/PostListSkeleton.vue', () => ({ default: { render: () => null } }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({
    success: (...a) => msg.success(...a),
    error: (...a) => msg.error(...a),
    warning: (...a) => msg.warning(...a),
    info: (...a) => msg.info(...a)
  }),
  useDialog: () => ({ warning: vi.fn() })
}))

const { default: CircleDetail } = await import('@/views/circle/CircleDetail.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const CIRCLE_ID = 'c1'

// 私密圈非成员的精简详情（后端 #170）
const restrictedDetail = () => ({
  id: CIRCLE_ID, name: '私密圈', avatar_url: '', join_type: 2, member_count: 12, restricted: true
})
const fullDetail = (extra = {}) => ({
  id: CIRCLE_ID,
  name: '公开圈',
  slug: 'open',
  description: '圈子简介正文',
  rule: '圈规正文',
  join_type: 0,
  member_count: 30,
  post_count: 5,
  hot: 1,
  create_time: '2026-01-01T00:00:00Z',
  is_joined: false,
  member_role: 0,
  member_status: 0,
  ...extra
})
const fail = (fields) => Promise.reject(Object.assign(new Error('backend english message'), fields))

let wrapper

const mountPage = async () => {
  const router = createTestRouter([
    { path: '/circle/:id' },
    { path: '/home' },
    { path: '/create-post' }
  ])
  await router.push(`/circle/${CIRCLE_ID}`)
  wrapper = mount(CircleDetail, {
    attachTo: document.body,
    global: { plugins: [router, createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

const buttonByText = (text) => wrapper.findAll('button').find(b => b.text().includes(text))

describe('CircleDetail：私密圈非成员空态（后端 #170）', () => {
  beforeEach(() => {
    vi.stubGlobal('IntersectionObserver', class { observe() {} disconnect() {} })
    vi.spyOn(console, 'error').mockImplementation(() => {})
    shell.mobile = false
    shareOrCopy.mockReset()
    authState.loggedIn = true
    api.getCirclePosts = vi.fn(() => Promise.resolve({ data: { posts: [{ id: 'p1' }], search_after: '' } }))
    api.joinCircle = vi.fn(() => Promise.resolve({ code: 200 }))
    api.leaveCircle = vi.fn(() => Promise.resolve({ code: 200 }))
    api.getModerationCounts = vi.fn(() => Promise.resolve({ reported: 0, pending: 0, full: false }))
    msg.success = vi.fn()
    msg.error = vi.fn()
    msg.warning = vi.fn()
    msg.info = vi.fn()
  })

  afterEach(() => {
    wrapper?.unmount()
    document.body.innerHTML = ''
  })

  it('restricted：展示「仅成员可见」精简页，不请求圈内帖子，不弹错误', async () => {
    api.getCircleDetail = vi.fn(() => Promise.resolve({ data: restrictedDetail() }))
    await mountPage()

    expect(api.getCirclePosts).not.toHaveBeenCalled()
    expect(wrapper.find('.members-only-state').exists()).toBe(true)
    expect(wrapper.text()).toContain('私密圈，仅成员可见')
    expect(wrapper.find('.post-list-stub').exists()).toBe(false)
    // 简介、圈规、创建时间等不渲染
    expect(wrapper.find('.description-section').exists()).toBe(false)
    expect(wrapper.find('.rules-section').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('暂无描述')
    expect(msg.error).not.toHaveBeenCalled()
  })

  it('restricted：不显示加入按钮，改为「私密圈，需邀请加入」提示（访客同样如此）', async () => {
    authState.loggedIn = false
    api.getCircleDetail = vi.fn(() => Promise.resolve({ data: restrictedDetail() }))
    await mountPage()

    expect(wrapper.find('.invite-only-hint').text()).toContain('私密圈，需邀请加入')
    expect(buttonByText('加入圈子')).toBeUndefined()
  })

  it('restricted：空态补充「仅限邀请加入」说明', async () => {
    api.getCircleDetail = vi.fn(() => Promise.resolve({ data: restrictedDetail() }))
    await mountPage()

    expect(wrapper.find('.members-only-invite').text()).toContain('仅限邀请加入')
  })

  it('私密圈成员：完整详情与帖子列表照常', async () => {
    api.getCircleDetail = vi.fn(() => Promise.resolve({
      data: fullDetail({ join_type: 2, is_joined: true, member_role: 10, member_status: 1 })
    }))
    await mountPage()

    expect(api.getCirclePosts).toHaveBeenCalledTimes(1)
    expect(wrapper.find('.members-only-state').exists()).toBe(false)
    expect(wrapper.find('.invite-only-hint').exists()).toBe(false)
    expect(wrapper.text()).toContain('圈子简介正文')
  })

  it('圈内帖子 403（例如已被移出）：显示「仅成员可见」空态，不弹通用错误', async () => {
    api.getCircleDetail = vi.fn(() => Promise.resolve({ data: fullDetail({ join_type: 2, is_joined: true }) }))
    api.getCirclePosts = vi.fn(() => fail({ code: 203, status: 403 }))
    await mountPage()

    expect(wrapper.find('.members-only-state').exists()).toBe(true)
    expect(msg.error).not.toHaveBeenCalled()
  })

  it('公开圈加入失败 403（已被拉黑）：前端文案，不展示后端英文 message', async () => {
    api.getCircleDetail = vi.fn(() => Promise.resolve({ data: fullDetail() }))
    api.joinCircle = vi.fn(() => fail({ code: 203, status: 403 }))
    await mountPage()

    await buttonByText('加入圈子').trigger('click')
    await flushPromises()

    expect(msg.error).toHaveBeenCalledWith('你已被该圈子拉黑，无法加入')
    expect(msg.error.mock.calls.flat().join()).not.toContain('backend english message')
  })

  it('加入返回 409（已是成员）：提示并重拉详情', async () => {
    api.getCircleDetail = vi.fn(() => Promise.resolve({ data: fullDetail() }))
    api.joinCircle = vi.fn(() => fail({ code: 207, status: 409 }))
    await mountPage()
    expect(api.getCircleDetail).toHaveBeenCalledTimes(1)

    await buttonByText('加入圈子').trigger('click')
    await flushPromises()

    expect(msg.error).toHaveBeenCalledWith('你已经是该圈子成员')
    expect(api.getCircleDetail).toHaveBeenCalledTimes(2)
  })

  it('管理入口角标只统计待审 / 被举报帖子，不再请求 status=0 的入圈待审', async () => {
    api.getCircleDetail = vi.fn(() => Promise.resolve({
      data: fullDetail({ is_joined: true, member_role: 30, member_status: 1 })
    }))
    api.getModerationCounts = vi.fn(() => Promise.resolve({ reported: 2, pending: 1, full: false }))
    await mountPage()

    expect(api.getModerationCounts).toHaveBeenCalledWith(CIRCLE_ID)
    expect(wrapper.find('.manage-badge').text()).toBe('3')
  })

  it('已开启的免打扰只在成员信息中显示，没有本地修改的按钮', async () => {
    api.getCircleDetail = vi.fn().mockResolvedValue({ data: fullDetail({ is_joined: true, member_is_disturb: 1 }) })
    await mountPage()
    expect(wrapper.find('.member-info').text()).toContain('消息免打扰')
    expect(buttonByText('免打扰')).toBeUndefined()
  })

  it.each([false, true])('圈子分享按钮使用详情链接，移动外框=%s', async (mobile) => {
    shell.mobile = mobile
    api.getCircleDetail = vi.fn().mockResolvedValue({ data: restrictedDetail() })
    shareOrCopy.mockResolvedValue('copied')
    await mountPage()
    await wrapper.get('button[aria-label="分享圈子"]').trigger('click')
    await flushPromises()
    expect(shareOrCopy).toHaveBeenCalledWith({ title: '私密圈', url: new URL('/circle/c1', location.origin).href })
    expect(msg.success).toHaveBeenCalledTimes(1)
    expect(api.getCirclePosts).not.toHaveBeenCalled()
    expect(buttonByText('举报')).toBeUndefined()
  })

  it.each([
    ['私密圈圈主', { join_type: 2, member_role: 30 }, true],
    ['私密圈管理员', { join_type: 2, member_role: 20 }, true],
    ['私密圈普通成员', { join_type: 2, member_role: 10 }, false],
    ['公开圈圈主', { join_type: 0, member_role: 30 }, false]
  ])('邀请成员入口：%s → %s', async (_, extra, visible) => {
    api.getCircleDetail = vi.fn(() => Promise.resolve({
      data: fullDetail({ is_joined: true, member_status: 1, ...extra })
    }))
    await mountPage()

    expect(wrapper.find('.invite-entry').exists()).toBe(visible)
  })
})
