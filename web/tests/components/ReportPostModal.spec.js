import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { NButton, NDrawer, NModal, NRadio, NRadioGroup, NInput } from 'naive-ui'

const mocks = vi.hoisted(() => ({
  reportPost: vi.fn(),
  message: { success: vi.fn(), warning: vi.fn(), error: vi.fn() }
}))

vi.mock('@/api/post', () => ({ reportPost: mocks.reportPost }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => mocks.message
}))

const { default: ReportPostModal } = await import('@/components/post/ReportPostModal.vue')
const { isPostReported, __resetPostReport } = await import('@/composables/usePostReport')
const { __resetBreakpoint } = await import('@/composables/useBreakpoint')
const { setMatchMedia } = await import('../setup')
const { createTestI18n } = await import('../helpers/plugins')

let wrapper

const mountModal = async (props = {}) => {
  wrapper = mount(ReportPostModal, {
    props: { show: true, postId: 'p1', ...props },
    attachTo: document.body,
    global: { plugins: [createTestI18n()] }
  })
  await flushPromises()
  return wrapper
}

const submitButton = () => wrapper.findAllComponents(NButton).find(b => b.classes().includes('report-submit'))
const pickReason = async (value) => {
  wrapper.findComponent(NRadioGroup).vm.$emit('update:value', value)
  await flushPromises()
}
const typeDetail = async (text) => {
  wrapper.findComponent(NInput).vm.$emit('update:value', text)
  await flushPromises()
}
const clickSubmit = async () => {
  submitButton().vm.$emit('click', new MouseEvent('click'))
  await flushPromises()
}
const bodyText = () => document.body.textContent

const httpError = (status, code, message) => Object.assign(new Error(message), { status, code })

describe('ReportPostModal', () => {
  beforeEach(() => {
    __resetPostReport()
    __resetBreakpoint()
    mocks.reportPost.mockReset()
    Object.values(mocks.message).forEach(fn => fn.mockReset())
    localStorage.setItem('qubar_token', 'tok')
    localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
  })

  afterEach(() => {
    wrapper?.unmount()
    document.body.innerHTML = ''
  })

  it('桌面：居中弹窗，列出 8 个理由、补充说明与匿名说明', async () => {
    await mountModal()
    expect(wrapper.findComponent(NModal).exists()).toBe(true)
    expect(wrapper.findComponent(NDrawer).exists()).toBe(false)
    expect(bodyText()).toContain('举报帖子')
    const labels = wrapper.findAllComponents(NRadio).map(r => r.text())
    expect(labels).toEqual(['垃圾广告 / 引流', '色情低俗', '辱骂骚扰', '违法违规', '暴力恐怖', '泄露隐私', '自伤轻生', '其他'])
    const input = wrapper.findComponent(NInput)
    expect(input.props('maxlength')).toBe(200)
    expect(input.props('showCount')).toBe(true)
    expect(input.props('placeholder')).toBe('补充说明（选填）')
    expect(bodyText()).toContain('举报人信息不会透露给作者')
  })

  it('移动端外框：改用底部抽屉', async () => {
    setMatchMedia(true)
    await mountModal()
    const drawer = wrapper.findComponent(NDrawer)
    expect(drawer.exists()).toBe(true)
    expect(drawer.props('placement')).toBe('bottom')
    expect(document.querySelector('.n-drawer.report-sheet .report-panel.is-sheet')).not.toBeNull()
  })

  it('未选理由：提交按钮禁用，也不会发请求', async () => {
    await mountModal()
    expect(submitButton().props('disabled')).toBe(true)
    await clickSubmit()
    expect(mocks.reportPost).not.toHaveBeenCalled()

    await pickReason(3)
    expect(submitButton().props('disabled')).toBe(false)
  })

  it('只在选「自伤轻生」时显示关怀提示', async () => {
    await mountModal()
    expect(document.querySelector('.report-care')).toBeNull()
    await pickReason(7)
    expect(document.querySelector('.report-care')?.textContent).toContain('心理援助热线')
    await pickReason(8)
    expect(document.querySelector('.report-care')).toBeNull()
  })

  it('提交参数：说明为空时不带 reason', async () => {
    mocks.reportPost.mockResolvedValue({ code: 200, data: null })
    await mountModal()
    await pickReason(1)
    await typeDetail('   ')
    await clickSubmit()
    expect(mocks.reportPost).toHaveBeenCalledWith({ post_id: 'p1', reason_type: 1 })
  })

  it('提交参数：说明去掉首尾空白后带上', async () => {
    mocks.reportPost.mockResolvedValue({ code: 200, data: null })
    await mountModal()
    await pickReason(8)
    await typeDetail('  评论区引流加微信 ')
    await clickSubmit()
    expect(mocks.reportPost).toHaveBeenCalledWith({ post_id: 'p1', reason_type: 8, reason: '评论区引流加微信' })
  })

  it('成功：提示并关闭，记入已举报', async () => {
    mocks.reportPost.mockResolvedValue({ code: 200, data: null })
    await mountModal()
    await pickReason(2)
    await clickSubmit()
    expect(mocks.message.success).toHaveBeenCalledWith('举报已提交，感谢反馈')
    expect(wrapper.emitted('update:show')).toEqual([[false]])
    expect(wrapper.emitted('reported')).toEqual([['p1']])
    expect(isPostReported('p1')).toBe(true)
  })

  it('提交中：按钮 loading，不能关闭、不能重复提交', async () => {
    let resolve
    mocks.reportPost.mockReturnValue(new Promise(r => { resolve = r }))
    await mountModal()
    await pickReason(2)
    await clickSubmit()
    expect(submitButton().props('loading')).toBe(true)
    await clickSubmit()
    wrapper.findComponent(NModal).vm.$emit('update:show', false)
    expect(mocks.reportPost).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('update:show')).toBeUndefined()

    resolve({ code: 200, data: null })
    await flushPromises()
    expect(submitButton().props('loading')).toBe(false)
    expect(wrapper.emitted('update:show')).toEqual([[false]])
  })

  it('409（code 207）：提示已举报，关闭并记入已举报', async () => {
    mocks.reportPost.mockRejectedValue(httpError(409, 207, 'You have already reported this post'))
    await mountModal()
    await pickReason(1)
    await clickSubmit()
    expect(mocks.message.warning).toHaveBeenCalledWith('你已经举报过这个帖子')
    expect(wrapper.emitted('update:show')).toEqual([[false]])
    expect(wrapper.emitted('reported')).toEqual([['p1']])
    expect(isPostReported('p1')).toBe(true)
  })

  it.each([
    ['code 208', httpError(429, 208, 'Report limit exceeded, please try again later')],
    ['只有 HTTP 429', httpError(429, undefined, 'Too Many Requests')]
  ])('429（%s）：提示太频繁，弹窗保留、内容不清空', async (_, error) => {
    mocks.reportPost.mockRejectedValue(error)
    await mountModal()
    await pickReason(5)
    await typeDetail('补充')
    await clickSubmit()
    expect(mocks.message.warning).toHaveBeenCalledWith('举报太频繁，请稍后再试')
    expect(wrapper.emitted('update:show')).toBeUndefined()
    expect(isPostReported('p1')).toBe(false)
    expect(wrapper.findComponent(NRadioGroup).props('value')).toBe(5)
    expect(wrapper.findComponent(NInput).props('value')).toBe('补充')
  })

  it('404（code 204）：中性提示并关闭，不记已举报', async () => {
    mocks.reportPost.mockRejectedValue(httpError(404, 204, 'Post not found or not published'))
    await mountModal()
    await pickReason(4)
    await clickSubmit()
    expect(mocks.message.warning).toHaveBeenCalledWith('帖子不存在或暂不可举报')
    expect(wrapper.emitted('update:show')).toEqual([[false]])
    expect(isPostReported('p1')).toBe(false)
  })

  it('400（code 201）：提示不能举报自己或参数有误，不展示后端英文 message', async () => {
    mocks.reportPost.mockRejectedValue(httpError(400, 201, 'You cannot report your own post'))
    await mountModal()
    await pickReason(6)
    await clickSubmit()
    expect(mocks.message.error).toHaveBeenCalledWith('不能举报自己的帖子，或参数有误')
    const shown = Object.values(mocks.message).flatMap(fn => fn.mock.calls.flat())
    expect(shown.join()).not.toMatch(/report/i)
    expect(wrapper.emitted('update:show')).toBeUndefined()
  })

  it('取消 / 关闭按钮：关闭弹窗；重新打开时表单清空', async () => {
    await mountModal()
    await pickReason(3)
    await typeDetail('x')
    const cancel = wrapper.findAllComponents(NButton).find(b => b.classes().includes('report-cancel'))
    cancel.vm.$emit('click', new MouseEvent('click'))
    expect(wrapper.emitted('update:show')).toEqual([[false]])

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    expect(wrapper.findComponent(NRadioGroup).props('value')).toBe(null)
    expect(wrapper.findComponent(NInput).props('value')).toBe('')
    expect(submitButton().props('disabled')).toBe(true)
  })
})
