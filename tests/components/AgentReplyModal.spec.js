import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { NButton, NInput } from 'naive-ui'
import AgentReplyModal from '@/components/agent/AgentReplyModal.vue'
import { createTestI18n, createTestRouter } from '../helpers/plugins'

const api = vi.hoisted(() => ({ global: vi.fn(), circle: vi.fn() }))
vi.mock('@/api/agent', () => ({ triggerAgentReply: api.global }))
vi.mock('@/api/circle', () => ({ triggerCircleAgentReply: api.circle }))

const post = '019a5d2e-7c41-7b0e-9f3a-2f6c1d8e4b10'
const comment = '019a7b10-2c3a-7d11-9e22-3f4a5b6c7d8e'
let wrapper
const open = (circle = false) => {
  wrapper = mount(AgentReplyModal, {
    props: { agent: { id: 'a1', name: 'Test Bot' }, circle },
    global: { plugins: [createTestI18n(), createTestRouter([{ path: '/' }, { path: '/post/:id', name: 'post-detail' }])] }
  })
}
const fill = async value => {
  wrapper.findComponent(NInput).vm.$emit('update:value', value)
  await flushPromises()
}
const click = () => wrapper.findAllComponents(NButton).find(b => b.props('type') === 'primary').vm.$emit('click')
const status = () => document.body.querySelector('[role="status"]').textContent
const href = () => document.body.querySelector('a').getAttribute('href')

describe('手动回复', () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(() => { wrapper?.unmount(); document.body.innerHTML = '' })

  it.each([false, true])('提交正确作用域，仅发一次，结果链接定位评论（圈内=%s）', async circle => {
    let finish
    const trigger = circle ? api.circle : api.global
    trigger.mockImplementationOnce(() => new Promise(resolve => { finish = resolve }))
    open(circle)
    await fill('bad-id')
    click()
    await flushPromises()
    expect(status()).toContain('有效的帖子 UUID')
    expect(trigger).not.toHaveBeenCalled()
    await fill(circle ? post : 'https://example.test/post/' + post + '?comment_id=old')
    click()
    click()
    await flushPromises()
    expect(trigger).toHaveBeenCalledExactlyOnceWith('a1', post)
    expect(wrapper.findComponent(NInput).props('disabled')).toBe(true)
    finish({ data: comment })
    await flushPromises()
    expect(status()).toBe('评论已发布。')
    expect(href()).toBe('/post/' + post + '?comment_id=' + comment)
  })

  it.each([
    [400, '未锁定'], [403, '权限'], [404, '管理范围'], [409, '已变化'],
    [429, '频率限制'], [503, '生成失败']
  ])('HTTP %i 显示对应动作且不重试', async (http, hint) => {
    api.circle.mockRejectedValueOnce(Object.assign(new Error('fixture'), { status: http }))
    open(true)
    await fill(post)
    click()
    await flushPromises()
    expect(status()).toContain(hint)
    expect(api.circle).toHaveBeenCalledTimes(1)
  })

  it.each([{ code: 'ECONNABORTED' }, { code: 'ERR_NETWORK' }, { status: 504 }])('同步结果不明时提示先核对，不再提交 %j', async error => {
    api.global.mockRejectedValueOnce(Object.assign(new Error('fixture'), error))
    open()
    await fill(post)
    click()
    await flushPromises()
    expect(status()).toContain('先到帖子中核对')
    expect(href()).toBe('/post/' + post)
    expect(wrapper.findAllComponents(NButton).some(b => b.props('type') === 'primary')).toBe(false)
    expect(api.global).toHaveBeenCalledTimes(1)
  })
})
