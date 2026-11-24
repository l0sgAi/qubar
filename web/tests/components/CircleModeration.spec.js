import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const api = vi.hoisted(() => ({
  getCircleDetail: null,
  getModerationPosts: null,
  getModerationReports: null,
  reviewModerationPost: null,
  resolveModerationReport: null,
  getModerationSummary: null
}))
const msg = vi.hoisted(() => ({ success: null, error: null, warning: null, info: null }))
const badge = vi.hoisted(() => ({ refresh: null }))

vi.mock('@/api/circle', () => ({ getCircleDetail: (...a) => api.getCircleDetail(...a) }))
vi.mock('@/api/moderation', () => ({
  getModerationPosts: (...a) => api.getModerationPosts(...a),
  getModerationReports: (...a) => api.getModerationReports(...a),
  reviewModerationPost: (...a) => api.reviewModerationPost(...a),
  resolveModerationReport: (...a) => api.resolveModerationReport(...a),
  getModerationSummary: (...a) => api.getModerationSummary(...a)
}))
vi.mock('@/composables/usePageTitle', () => ({ usePageTitle: () => ({ setTitleData: () => {} }) }))
vi.mock('@/components/layout/AppHeader.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/layout/SideNav.vue', () => ({ default: { render: () => null } }))
vi.mock('@/components/circle/CircleAdminHeader.vue', () => ({
  default: {
    setup(_, { expose }) {
      expose({ refreshModerationBadge: () => badge.refresh() })
      return () => null
    }
  }
}))
vi.mock('@/components/post/detail/MentionPreview.vue', () => ({
  default: { props: ['content'], template: '<div class="md-stub">{{ content }}</div>' }
}))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => ({
    success: (...a) => msg.success(...a),
    error: (...a) => msg.error(...a),
    warning: (...a) => msg.warning(...a),
    info: (...a) => msg.info(...a)
  })
}))

const { default: CircleModeration } = await import('@/views/circle/CircleModeration.vue')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const CIRCLE_ID = 'c1'

const post = (id, extra = {}) => ({
  id,
  circle_id: CIRCLE_ID,
  user_id: 'u1',
  title: `标题 ${id}`,
  summary: '',
  content: '',
  media_extra: [],
  status: 1,
  create_time: new Date().toISOString(),
  author_name: '作者',
  author_avatar: '',
  ai_verdict: null,
  ...extra
})

const reported = (id, extra = {}) => post(id, {
  report_count: 2,
  first_report_time: new Date().toISOString(),
  reports: [
    { reason_type: 1, reason: '评论区引流加微信', create_time: new Date().toISOString() },
    { reason_type: 3, reason: '', create_time: new Date().toISOString() }
  ],
  ...extra
})

const page = (posts, cursor = '') => Promise.resolve({ code: 200, data: { posts, size: 20, cursor } })
const fail = (code) => Promise.reject(Object.assign(new Error('x'), { code }))

let wrapper
let router

const mountPage = async (query = '') => {
  router = createTestRouter([
    { path: '/circle/:id' },
    { path: '/circle/:id/moderation', name: 'circle-moderation' },
    { path: '/user/:id' }
  ])
  await router.push(`/circle/${CIRCLE_ID}/moderation${query}`)
  wrapper = mount(CircleModeration, {
    attachTo: document.body,
    global: { plugins: [router, createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

const tabs = () => wrapper.findAll('.n-tabs-tab')
const tabText = (key) => tabs().find(el => el.attributes('data-name') === key)?.text()
const items = () => wrapper.findAll('.mod-item')
const modalButton = () => document.body.querySelector('.review-confirm')
const modalText = () => document.body.querySelector('.review-target')?.textContent

// 点列表项按钮：0 = 破坏性动作（下架 / 驳回），1 = 主动作（保留 / 通过）
const clickAction = async (itemIdx, btnIdx) => {
  await items()[itemIdx].findAll('.mod-actions button')[btnIdx].trigger('click')
  await flushPromises()
}

const typeReason = async (text) => {
  const textarea = document.body.querySelector('.review-body textarea')
  textarea.value = text
  textarea.dispatchEvent(new Event('input'))
  await flushPromises()
}

const confirm = async () => {
  modalButton().click()
  await flushPromises()
}

describe('CircleModeration：被举报 Tab（#31）', () => {
  beforeEach(() => {
    vi.stubGlobal('IntersectionObserver', class { observe() {} disconnect() {} })
    // 失败路径会 console.error，属预期输出
    vi.spyOn(console, 'error').mockImplementation(() => {})
    api.getCircleDetail = vi.fn(() => Promise.resolve({ data: { id: CIRCLE_ID, name: '圈', member_role: 30 } }))
    api.getModerationReports = vi.fn(() => page([reported('r1'), reported('r2')]))
    api.getModerationPosts = vi.fn(() => page([post('p1')]))
    api.reviewModerationPost = vi.fn(() => Promise.resolve({ data: { post_id: 'p1', status: 1 } }))
    api.resolveModerationReport = vi.fn(() => Promise.resolve({ data: { post_id: 'r1', status: 1, closed_reports: 2 } }))
    msg.success = vi.fn()
    msg.error = vi.fn()
    msg.warning = vi.fn()
    msg.info = vi.fn()
    badge.refresh = vi.fn()
    // summary 接口（#156）默认未上线：404
    api.getModerationSummary = vi.fn(() => Promise.reject(Object.assign(new Error('x'), { status: 404 })))
    Element.prototype.scrollIntoView = vi.fn()
  })

  afterEach(() => {
    wrapper?.unmount()
    document.body.innerHTML = ''
  })

  it('默认打开「被举报」，两个队列各拉首页，Tab 带待处理数角标', async () => {
    await mountPage()
    expect(tabs().map(el => el.attributes('data-name'))).toEqual(['reported', 'pending'])
    expect(wrapper.find('.n-tabs-tab--active').attributes('data-name')).toBe('reported')
    expect(api.getModerationReports).toHaveBeenCalledWith({ circle_id: CIRCLE_ID, size: 20 })
    expect(api.getModerationPosts).toHaveBeenCalledWith({ circle_id: CIRCLE_ID, size: 20 })
    expect(tabText('reported')).toBe('被举报2')
    expect(tabText('pending')).toBe('待审帖子1')
    // 举报信息与按钮
    expect(items()).toHaveLength(2)
    expect(items()[0].find('.report-count').text()).toBe('被举报 2 次')
    expect(items()[0].findAll('.report-group').map(w => w.text())).toEqual(['垃圾广告 / 引流', '辱骂骚扰'])
    expect(items()[0].findAll('.mod-actions button').map(w => w.text())).toEqual(['下架', '保留'])
  })

  it('满页（有下一页）时角标显示「20+」', async () => {
    api.getModerationReports = vi.fn(() => page([reported('r1')], 'next'))
    await mountPage()
    expect(tabText('reported')).toBe('被举报20+')
  })

  it('status=4 显示「已自动屏蔽」标签与说明', async () => {
    api.getModerationReports = vi.fn(() => page([reported('r1', { status: 4 })]))
    await mountPage()
    expect(items()[0].find('.hidden-tag').text()).toBe('已自动屏蔽')
    expect(items()[0].find('.report-hidden-hint').text()).toBe('仅作者可见，保留后恢复公开')
  })

  it('空态：被举报为空且待审有数据时自动切到待审；?tab=reported 时不自动切', async () => {
    api.getModerationReports = vi.fn(() => page([]))
    await mountPage()
    expect(wrapper.find('.n-tabs-tab--active').attributes('data-name')).toBe('pending')
    wrapper.unmount()

    await mountPage('?tab=reported')
    expect(wrapper.find('.n-tabs-tab--active').attributes('data-name')).toBe('reported')
    expect(wrapper.find('.list-empty').text()).toBe('暂无被举报的帖子')
  })

  it.each([['reports', 'reported'], ['review', 'pending']])('?tab=%s（对接文档写法）映射到 %s', async (tab, key) => {
    await mountPage(`?tab=${tab}`)
    expect(wrapper.find('.n-tabs-tab--active').attributes('data-name')).toBe(key)
  })

  it('?tab=pending 定位到待审 Tab', async () => {
    await mountPage('?tab=pending')
    expect(wrapper.find('.n-tabs-tab--active').attributes('data-name')).toBe('pending')
    expect(items()[0].findAll('.mod-actions button').map(w => w.text())).toEqual(['驳回', '通过'])
  })

  it('保留：带备注提交 dismiss，提示关闭条数，本地移除并同步角标', async () => {
    await mountPage()
    await clickAction(0, 1)
    expect(modalText()).toContain('确定保留「标题 r1」吗？')
    await typeReason('  举报不成立  ')
    await confirm()
    expect(api.resolveModerationReport).toHaveBeenCalledWith({
      circle_id: CIRCLE_ID, post_id: 'r1', action: 'dismiss', reason: '举报不成立'
    })
    expect(msg.success).toHaveBeenCalledWith('已保留，关闭 2 条举报')
    expect(items().map(w => w.find('.mod-title').text())).toEqual(['标题 r2'])
    expect(tabText('reported')).toBe('被举报1')
    expect(badge.refresh).toHaveBeenCalled()
  })

  it('下架：弹二次确认（说明仅作者可见并通知作者），不填备注时不传 reason', async () => {
    api.resolveModerationReport = vi.fn(() => Promise.resolve({ data: { post_id: 'r1', status: 3, closed_reports: 2 } }))
    await mountPage()
    await clickAction(0, 0)
    // 打开弹窗不会直接提交
    expect(api.resolveModerationReport).not.toHaveBeenCalled()
    expect(modalText()).toContain('下架后仅作者可见，作者会收到通知')
    expect(document.body.querySelector('.review-body .n-input-word-count')).not.toBeNull()
    await confirm()
    expect(api.resolveModerationReport).toHaveBeenCalledWith({ circle_id: CIRCLE_ID, post_id: 'r1', action: 'remove' })
    expect(msg.success).toHaveBeenCalledWith('已下架')
    expect(items()).toHaveLength(1)
  })

  it('提交期间按钮 loading，重复点击不会二次提交', async () => {
    let resolve
    api.resolveModerationReport = vi.fn(() => new Promise((r) => { resolve = r }))
    await mountPage()
    await clickAction(0, 1)
    await confirm()
    await confirm()
    expect(api.resolveModerationReport).toHaveBeenCalledTimes(1)
    expect(modalButton().className).toContain('n-button--loading')
    resolve({ data: { closed_reports: 1 } })
    await flushPromises()
    expect(msg.success).toHaveBeenCalledWith('已保留，关闭 1 条举报')
  })

  it('404（204）：提示已被其他管理员处理并刷新被举报列表', async () => {
    api.resolveModerationReport = vi.fn(() => fail(204))
    await mountPage()
    api.getModerationReports.mockClear()
    await clickAction(0, 0)
    await confirm()
    expect(msg.warning).toHaveBeenCalledWith('该帖子已被其他管理员处理')
    expect(api.getModerationReports).toHaveBeenCalledWith({ circle_id: CIRCLE_ID, size: 20 })
    expect(api.getModerationPosts).toHaveBeenCalledTimes(1)
  })

  it('403（203）：提示无权限并退回圈子详情（本地角色仍是管理员，如被禁言）', async () => {
    api.resolveModerationReport = vi.fn(() => fail(203))
    await mountPage()
    await clickAction(0, 1)
    await confirm()
    expect(msg.error).toHaveBeenCalledWith('无权限执行该操作')
    expect(router.currentRoute.value.path).toBe(`/circle/${CIRCLE_ID}`)
  })

  it('两个队列同时 403 只提示一次', async () => {
    api.getModerationReports = vi.fn(() => fail(203))
    api.getModerationPosts = vi.fn(() => fail(203))
    await mountPage()
    expect(msg.error).toHaveBeenCalledTimes(1)
    expect(msg.error).toHaveBeenCalledWith('无权限执行该操作')
  })

  it('游标非法（201）时从首页重拉', async () => {
    api.getModerationReports = vi.fn()
      .mockImplementationOnce(() => page([reported('r1')], 'c-1'))
      .mockImplementationOnce(() => fail(201))
      .mockImplementation(() => page([reported('r9')]))
    await mountPage()
    await wrapper.findAll('.list-footer button')[0].trigger('click')
    await flushPromises()
    expect(api.getModerationReports.mock.calls.map(([p]) => p.cursor)).toEqual([undefined, 'c-1', undefined])
    expect(items().map(w => w.find('.mod-title').text())).toEqual(['标题 r9'])
    expect(msg.error).not.toHaveBeenCalled()
  })

  it('非圈管不发列表请求，退回圈子详情', async () => {
    api.getCircleDetail = vi.fn(() => Promise.resolve({ data: { id: CIRCLE_ID, member_role: 10 } }))
    await mountPage()
    expect(api.getModerationReports).not.toHaveBeenCalled()
    expect(api.getModerationPosts).not.toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe(`/circle/${CIRCLE_ID}`)
  })

  it('待审 Tab 的通过仍走 /circle/moderation/review', async () => {
    await mountPage('?tab=pending')
    await clickAction(0, 1)
    await confirm()
    expect(api.reviewModerationPost).toHaveBeenCalledWith({ circle_id: CIRCLE_ID, post_id: 'p1', action: 'approve' })
    expect(api.resolveModerationReport).not.toHaveBeenCalled()
    expect(msg.success).toHaveBeenCalledWith('已通过，帖子将在几秒内出现在圈子中')
  })

  it('summary 接口可用时 Tab 角标用准确数字（不再显示 20+），处理后 -1', async () => {
    api.getModerationSummary = vi.fn(() => Promise.resolve({ data: { report_pending: 25, review_pending: 0 } }))
    api.getModerationReports = vi.fn(() => page([reported('r1'), reported('r2')], 'next'))
    await mountPage()
    expect(api.getModerationSummary).toHaveBeenCalledWith({ circle_id: CIRCLE_ID })
    expect(tabText('reported')).toBe('被举报25')
    expect(tabText('pending')).toBe('待审帖子')
    await clickAction(0, 1)
    await confirm()
    expect(tabText('reported')).toBe('被举报24')
  })

  describe('?focus= 定位（通知跳转）', () => {
    const focused = () => wrapper.findAll('.mod-item.is-focused')

    it('首页命中：切到被举报 Tab，高亮并滚动到该帖子，URL 去掉 focus', async () => {
      await mountPage('?tab=pending&focus=r2')
      expect(wrapper.find('.n-tabs-tab--active').attributes('data-name')).toBe('reported')
      expect(focused().map(w => w.find('.mod-title').text())).toEqual(['标题 r2'])
      expect(Element.prototype.scrollIntoView).toHaveBeenCalledTimes(1)
      expect(router.currentRoute.value.query.focus).toBeUndefined()
      expect(router.currentRoute.value.query.tab).toBe('reported')
      expect(msg.info).not.toHaveBeenCalled()
    })

    it('首页没有就继续翻页，在第 2 页命中', async () => {
      api.getModerationReports = vi.fn()
        .mockImplementationOnce(() => page([reported('r1')], 'c-1'))
        .mockImplementation(() => page([reported('r7')], 'c-2'))
      await mountPage('?tab=reported&focus=r7')
      expect(api.getModerationReports.mock.calls.map(([p]) => p.cursor)).toEqual([undefined, 'c-1'])
      expect(focused().map(w => w.find('.mod-title').text())).toEqual(['标题 r7'])
    })

    it('最多翻 3 页仍找不到：提示「该举报已处理」，不再继续翻', async () => {
      let n = 0
      api.getModerationReports = vi.fn(() => { n++; return page([reported(`r${n}`)], `c-${n}`) })
      await mountPage('?tab=reported&focus=gone')
      expect(api.getModerationReports).toHaveBeenCalledTimes(3)
      expect(msg.info).toHaveBeenCalledWith('该举报已处理')
      expect(focused()).toHaveLength(0)
      expect(router.currentRoute.value.query.focus).toBeUndefined()
    })

    it('列表没有更多时直接提示，不多发请求', async () => {
      await mountPage('?tab=reported&focus=gone')
      expect(api.getModerationReports).toHaveBeenCalledTimes(1)
      expect(msg.info).toHaveBeenCalledWith('该举报已处理')
    })

    it('翻页失败时停止定位，只提示加载失败', async () => {
      api.getModerationReports = vi.fn()
        .mockImplementationOnce(() => page([reported('r1')], 'c-1'))
        .mockImplementation(() => fail(500))
      await mountPage('?tab=reported&focus=r9')
      expect(api.getModerationReports).toHaveBeenCalledTimes(2)
      expect(msg.error).toHaveBeenCalledWith('被举报帖子加载失败')
    })
  })
})
