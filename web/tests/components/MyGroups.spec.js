import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { NInput } from 'naive-ui'

const mocks = vi.hoisted(() => ({
  getMyCircles: vi.fn(),
  getUserCircles: vi.fn()
}))

vi.mock('@/api/post', () => ({
  getMyCircles: mocks.getMyCircles,
  getUserCircles: mocks.getUserCircles
}))

const { default: MyGroups } = await import('@/components/user/MyGroups.vue')
const { __resetBreakpoint } = await import('@/composables/useBreakpoint')
const { setMatchMedia } = await import('../setup')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

// 只伪造定时器与 Date：flushPromises 依赖的 setImmediate 保持真实
const FAKE_TIMERS = { toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'] }

const ok = (data) => Promise.resolve({ code: 200, data })
const page = (circles, extra = {}) => ok({ circles, total: circles.length, size: 20, search_after: '', ...extra })
const httpError = (code, message) => Object.assign(new Error(message), { code })

const circle = (overrides = {}) => ({
  id: 'c1',
  name: '摄影',
  avatar_url: 'https://img/c1.png',
  member_count: 12,
  slug: 'photo',
  role: 10,
  member_status: 1,
  ...overrides
})

// IntersectionObserver 桩：记录回调，用例里手动触发「哨兵可见」
let observerCallback = null
class FakeObserver {
  constructor(cb) { observerCallback = cb }
  observe() {}
  disconnect() {}
}
const scrollToBottom = async () => {
  observerCallback?.([{ isIntersecting: true }])
  await flushPromises()
}

let wrapper

const mountGroups = async (props = {}) => {
  const router = createTestRouter([
    { path: '/' },
    { path: '/circle/:id', name: 'circle-detail' },
    { path: '/circle/:id/moderation', name: 'circle-moderation' }
  ])
  wrapper = mount(MyGroups, {
    props: { active: true, ...props },
    global: { plugins: [createTestI18n(), router] }
  })
  await flushPromises()
  return wrapper
}

const lastParams = (fn) => fn.mock.calls.at(-1).at(-1)
const roleTab = (label) => wrapper.findAll('.role-filter__item').find(b => b.text().startsWith(label))
const cards = () => wrapper.findAll('.group-card')

describe('MyGroups', () => {
  beforeEach(() => {
    __resetBreakpoint()
    observerCallback = null
    vi.stubGlobal('IntersectionObserver', FakeObserver)
    vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.getMyCircles.mockReset()
    mocks.getUserCircles.mockReset()
  })

  afterEach(() => {
    wrapper?.unmount()
    wrapper = null
  })

  it('我的圈子：展示 slug、圈主 / 管理员徽标，普通成员无徽标；首屏上报总数', async () => {
    mocks.getMyCircles.mockReturnValue(page([
      circle({ id: 'c1', name: '摄影', role: 30 }),
      circle({ id: 'c2', name: '徒步', role: 20, slug: 'hiking' }),
      circle({ id: 'c3', name: '读书', role: 10, slug: undefined, avatar_url: undefined })
    ]))
    await mountGroups()

    expect(mocks.getMyCircles).toHaveBeenCalledWith({ size: 20 })
    const [owner, admin, member] = cards()
    expect(owner.find('.group-role').text()).toBe('圈主')
    expect(owner.find('.group-slug').text()).toBe('c/photo')
    expect(admin.find('.group-role').text()).toBe('管理员')
    expect(admin.find('.group-slug').text()).toBe('c/hiking')
    // 普通成员、无 slug、无头像：不渲染徽标与 slug，头像回退首字母
    expect(member.find('.group-role').exists()).toBe(false)
    expect(member.find('.group-slug').exists()).toBe(false)
    expect(member.find('.group-dot').exists()).toBe(false)
    expect(member.find('.group-info').text()).toContain('读')
    // 紧凑布局：slug 与成员数同一行（#48）
    expect(owner.find('.group-sub .group-slug').text()).toBe('c/photo')
    expect(owner.find('.group-sub .group-stat').text()).toBe('12')
    expect(member.find('.group-sub').text().trim()).toBe('12')
    expect(member.find('.post-cover-link').attributes('href')).toBe('/circle/c3')

    expect(wrapper.emitted('total-change')).toEqual([[3]])
    // 角色 Tab 不显示数量，总数只上报给父页面的「兴趣圈」Tab
    expect(wrapper.findAll('.role-filter__item').map(b => b.text())).toEqual(['全部', '圈主', '管理', '成员'])
  })

  it('我的圈子：圈主 / 管理员有「管理」入口（跳内容审核页），普通成员没有；移动端隐藏', async () => {
    mocks.getMyCircles.mockReturnValue(page([
      circle({ id: 'c1', role: 30 }),
      circle({ id: 'c2', role: 20 }),
      circle({ id: 'c3', role: 10 })
    ]))
    await mountGroups()

    const links = cards().map(c => c.find('.group-manage'))
    expect(links[0].attributes('href')).toBe('/circle/c1/moderation')
    expect(links[1].attributes('href')).toBe('/circle/c2/moderation')
    expect(links[2].exists()).toBe(false)
    // 管理入口在第二行右端，不占名称行宽度、不再绝对定位压住名称（#48）
    expect(cards()[0].find('.group-sub .group-manage').exists()).toBe(true)
    expect(cards()[0].find('.group-title .group-manage').exists()).toBe(false)

    setMatchMedia(true)
    await flushPromises()
    expect(wrapper.find('.group-manage').exists()).toBe(false)
  })

  it('禁言中显示截止时间，到期后不刷新也恢复为正常', async () => {
    vi.useFakeTimers(FAKE_TIMERS)
    vi.setSystemTime(new Date('2026-10-25T10:00:00Z'))
    mocks.getMyCircles.mockReturnValue(page([
      circle({ member_status: 2, mute_end_time: '2026-10-25T10:01:30Z' })
    ]))
    await mountGroups()

    expect(wrapper.find('.group-muted').text()).toMatch(/^禁言中 · 至 /)
    // 禁言标签跟在第二行（slug · 成员数）末尾，不单独占一行
    expect(wrapper.find('.group-sub .group-muted').exists()).toBe(true)

    vi.advanceTimersByTime(120_000)
    await flushPromises()
    expect(wrapper.find('.group-muted').exists()).toBe(false)
  })

  it('他人主页：走 /circle/user，展示角色徽标但不展示禁言与管理入口', async () => {
    mocks.getUserCircles.mockReturnValue(page([
      // 即便字段意外出现，也不在他人主页展示
      circle({ role: 30, member_status: 2, mute_end_time: '2999-01-01T00:00:00Z' })
    ]))
    await mountGroups({ userId: 'u9' })

    expect(mocks.getUserCircles).toHaveBeenCalledWith('u9', { size: 20 })
    expect(mocks.getMyCircles).not.toHaveBeenCalled()
    expect(wrapper.find('.group-role').text()).toBe('圈主')
    expect(wrapper.find('.group-muted').exists()).toBe(false)
    expect(wrapper.find('.group-manage').exists()).toBe(false)
  })

  it('空状态按身份与角色 Tab 区分文案', async () => {
    mocks.getUserCircles.mockReturnValue(page([]))
    await mountGroups({ userId: 'u9' })
    expect(wrapper.find('.empty-text').text()).toBe('TA 还没有加入圈子')
    expect(wrapper.find('.empty-hint').exists()).toBe(false)

    await roleTab('圈主').trigger('click')
    await flushPromises()
    expect(wrapper.find('.empty-text').text()).toBe('TA 还不是任何圈子的圈主')
  })

  it('切换角色 Tab：丢弃旧游标从首页请求，Tab 不显示数量，不覆盖父页面总数', async () => {
    mocks.getMyCircles.mockReturnValueOnce(page([circle()], { total: 30, search_after: 'cursor-all' }))
    await mountGroups()

    mocks.getMyCircles.mockReturnValueOnce(page([circle({ id: 'c9', role: 30 })], { total: 2, search_after: 'cursor-owner' }))
    await roleTab('圈主').trigger('click')
    await flushPromises()

    expect(lastParams(mocks.getMyCircles)).toEqual({ size: 20, role: 30 })
    expect(cards()).toHaveLength(1)
    expect(roleTab('圈主').text()).toBe('圈主')
    expect(roleTab('全部').text()).toBe('全部')
    expect(wrapper.emitted('total-change')).toEqual([[30]])

    // 翻页带的是本角色的游标
    mocks.getMyCircles.mockReturnValueOnce(page([circle({ id: 'c10', role: 30 })]))
    await scrollToBottom()
    expect(lastParams(mocks.getMyCircles)).toEqual({ size: 20, role: 30, search_after: 'cursor-owner' })
    expect(cards()).toHaveLength(2)
  })

  it('关键字输入防抖后从首页请求，可与角色组合；关键字模式不上报总数', async () => {
    vi.useFakeTimers(FAKE_TIMERS)
    mocks.getMyCircles.mockReturnValueOnce(page([circle()], { total: 30, search_after: 'cursor-all' }))
    await mountGroups()

    mocks.getMyCircles.mockReturnValue(page([circle()], { total: 0, search_after: 'cursor-kw' }))
    const input = wrapper.findComponent(NInput)
    input.vm.$emit('update:value', '摄')
    input.vm.$emit('update:value', '摄影')
    await flushPromises()
    vi.advanceTimersByTime(299)
    expect(mocks.getMyCircles).toHaveBeenCalledTimes(1)

    vi.advanceTimersByTime(1)
    await flushPromises()
    expect(mocks.getMyCircles).toHaveBeenCalledTimes(2)
    expect(lastParams(mocks.getMyCircles)).toEqual({ size: 20, keyword: '摄影' })

    await roleTab('管理').trigger('click')
    await flushPromises()
    expect(lastParams(mocks.getMyCircles)).toEqual({ size: 20, keyword: '摄影', role: 20 })
    // 关键字模式 total 恒为 0，不上报给父页面
    expect(wrapper.emitted('total-change')).toEqual([[30]])
  })

  it('清空关键字立即恢复全量列表', async () => {
    vi.useFakeTimers(FAKE_TIMERS)
    mocks.getMyCircles.mockReturnValue(page([circle()]))
    await mountGroups()
    const input = wrapper.findComponent(NInput)
    input.vm.$emit('update:value', '摄影')
    await flushPromises()
    vi.advanceTimersByTime(300)
    await flushPromises()

    input.vm.$emit('update:value', '')
    await flushPromises()
    expect(lastParams(mocks.getMyCircles)).toEqual({ size: 20 })
    expect(mocks.getMyCircles).toHaveBeenCalledTimes(3)
  })

  it('翻页返回 400 游标错误时丢弃游标，重新加载第一页', async () => {
    mocks.getMyCircles.mockReturnValueOnce(page([circle()], { total: 30, search_after: 'stale' }))
    await mountGroups()

    mocks.getMyCircles
      .mockReturnValueOnce(Promise.reject(httpError(400, 'Invalid search_after parameter')))
      .mockReturnValueOnce(page([circle({ id: 'c2' })], { total: 30 }))
    await scrollToBottom()

    expect(mocks.getMyCircles).toHaveBeenCalledTimes(3)
    expect(lastParams(mocks.getMyCircles)).toEqual({ size: 20 })
    expect(cards()).toHaveLength(1)
    expect(wrapper.find('.load-more-error').exists()).toBe(false)
  })

  it('旧条件的响应晚到时丢弃，不会串进新列表', async () => {
    let resolveAll
    mocks.getMyCircles.mockReturnValueOnce(new Promise(r => { resolveAll = r }))
    await mountGroups()

    mocks.getMyCircles.mockReturnValueOnce(page([circle({ id: 'owner', name: '我建的', role: 30 })]))
    await roleTab('圈主').trigger('click')
    await flushPromises()

    resolveAll({ code: 200, data: { circles: [circle({ id: 'old', name: '旧结果' })], total: 9, search_after: 'x' } })
    await flushPromises()

    expect(cards()).toHaveLength(1)
    expect(wrapper.text()).toContain('我建的')
    expect(wrapper.text()).not.toContain('旧结果')
    expect(wrapper.emitted('total-change')).toBeUndefined()
  })

  it('首屏失败显示重试按钮，点击后重新加载；401 交给拦截器不显示错误', async () => {
    mocks.getMyCircles.mockReturnValueOnce(Promise.reject(httpError(500, 'boom')))
    await mountGroups()
    expect(wrapper.find('.empty-text').text()).toBe('圈子列表加载失败')

    mocks.getMyCircles.mockReturnValueOnce(page([circle()]))
    await wrapper.find('.retry-btn').trigger('click')
    await flushPromises()
    expect(cards()).toHaveLength(1)

    wrapper.unmount()
    mocks.getMyCircles.mockReturnValueOnce(Promise.reject(httpError(401, 'unauthorized')))
    await mountGroups()
    expect(wrapper.text()).not.toContain('圈子列表加载失败')
  })

  it('离开 Tab 时清空关键字与角色筛选并重拉全量首页', async () => {
    mocks.getMyCircles.mockReturnValue(page([circle()]))
    await mountGroups()
    await roleTab('成员').trigger('click')
    await flushPromises()
    expect(lastParams(mocks.getMyCircles)).toEqual({ size: 20, role: 10 })

    await wrapper.setProps({ active: false })
    await flushPromises()
    expect(lastParams(mocks.getMyCircles)).toEqual({ size: 20 })
    expect(roleTab('全部').classes()).toContain('role-filter__item--active')
  })
})
