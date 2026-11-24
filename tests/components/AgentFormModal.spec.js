import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { NButton, NSwitch, NCheckbox } from 'naive-ui'

const mocks = vi.hoisted(() => ({
  createAgent: vi.fn(),
  updateAgent: vi.fn(),
  createCircleAgent: vi.fn(),
  updateCircleAgent: vi.fn(),
  message: { success: vi.fn(), warning: vi.fn(), error: vi.fn() }
}))

vi.mock('@/api/agent', () => ({ createAgent: mocks.createAgent, updateAgent: mocks.updateAgent }))
vi.mock('@/api/circle', () => ({
  createCircleAgent: mocks.createCircleAgent,
  updateCircleAgent: mocks.updateCircleAgent
}))
vi.mock('@/composables/useImageUpload', () => ({ useImageUpload: () => ({ uploadOne: vi.fn() }) }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => mocks.message
}))

const { default: AgentFormModal } = await import('@/components/agent/AgentFormModal.vue')
const { createTestI18n } = await import('../helpers/plugins')

const autoReply = (over = {}) => ({
  enabled: true,
  targets: ['post', 'comment'],
  post_hot_min: 200,
  comment_like_min: 20,
  comment_reply_min: 0,
  window_hours: 24,
  max_per_hour: 5,
  ...over
})

const agentVO = (over = {}) => ({
  id: 'a1',
  name: 'bot',
  avatar_url: '',
  api_protocol: 'openai',
  base_url: '',
  has_api_key: false,
  model: 'gpt-4o-mini',
  llm_params: {},
  system_prompt: '',
  filter_prompt: '',
  trigger_mode: 2,
  trigger_keywords: ['hi'],
  max_replies_per_hour: 30,
  min_interval_sec: 60,
  ...over
})

let wrapper

// 弹窗按 show 的变化初始化表单，故先以 show=false 挂载再打开
const openModal = async (props = {}) => {
  wrapper = mount(AgentFormModal, {
    props: { show: false, ...props },
    attachTo: document.body,
    global: {
      plugins: [createTestI18n()],
      stubs: { ImageCropperModal: true }
    }
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

const form = () => wrapper.vm.formData
const bodyText = () => document.body.textContent
const toggle = async on => {
  wrapper.findComponent(NSwitch).vm.$emit('update:value', on)
  await flushPromises()
}
const submit = async () => {
  const btn = wrapper.findAllComponents(NButton).find(b => b.props('type') === 'primary')
  btn.vm.$emit('click', new MouseEvent('click'))
  await flushPromises()
}

describe('AgentFormModal 自动回复', () => {
  beforeEach(() => {
    Object.values(mocks).forEach(m => typeof m === 'function' && m.mockReset())
    Object.values(mocks.message).forEach(fn => fn.mockReset())
    mocks.createAgent.mockResolvedValue({ data: {} })
    mocks.updateAgent.mockResolvedValue({ data: {} })
    mocks.updateCircleAgent.mockResolvedValue({ data: {} })
    mocks.createCircleAgent.mockResolvedValue({ data: {} })
  })

  afterEach(() => {
    wrapper?.unmount()
    document.body.innerHTML = ''
  })

  it('编辑：回显已有配置并展开字段，展示说明', async () => {
    await openModal({ agent: agentVO({ auto_config: { auto_reply: autoReply() } }) })
    expect(form().auto_reply).toEqual(autoReply())
    expect(wrapper.findComponent(NSwitch).props('value')).toBe(true)
    expect(wrapper.findAllComponents(NCheckbox).map(c => c.text())).toContain('评论（回复到该评论楼层）')
    expect(bodyText()).toContain('帖子热度门槛')
    expect(bodyText()).toContain('每个机器人对同一目标最多自动回复一次')
    expect(bodyText()).toContain('全局机器人扫描全站内容')
  })

  it('编辑：只改自动回复时提交完整 auto_reply 对象，保留已配置的 auto_post', async () => {
    await openModal({ agent: agentVO({ auto_config: { auto_reply: autoReply(), auto_post: autoPost() } }) })
    form().auto_reply.max_per_hour = 8
    await submit()
    expect(mocks.updateAgent).toHaveBeenCalledWith('a1', {
      auto_config: { auto_reply: autoReply({ max_per_hour: 8 }) }
    })
  })

  it('编辑：未改自动回复时不提交 auto_config（旧后端无该字段也一样）', async () => {
    await openModal({ agent: agentVO({ api_protocol: 'ollama', max_replies_per_hour: undefined, min_interval_sec: undefined }) })
    expect(wrapper.findComponent(NSwitch).props('value')).toBe(false)
    expect(bodyText()).not.toContain('帖子热度门槛')
    form().name = 'bot2'
    await submit()
    expect(mocks.updateAgent).toHaveBeenCalledWith('a1', { name: 'bot2' })
  })

  it('关闭：折叠字段，但提交时带上保留的完整配置', async () => {
    await openModal({ agent: agentVO({ auto_config: { auto_reply: autoReply() } }) })
    await toggle(false)
    expect(bodyText()).not.toContain('帖子热度门槛')
    await submit()
    expect(mocks.updateAgent).toHaveBeenCalledWith('a1', {
      auto_config: { auto_reply: autoReply({ enabled: false }) }
    })
  })

  it('首次开启：预填目标与窗口；缺帖子门槛时拦截提交', async () => {
    await openModal({ agent: agentVO() })
    await toggle(true)
    expect(form().auto_reply.targets).toEqual(['post'])
    expect(form().auto_reply.window_hours).toBe(24)
    await submit()
    expect(mocks.updateAgent).not.toHaveBeenCalled()
    expect(bodyText()).toContain('回复帖子时热度门槛须 ≥ 1')
  })

  it('评论目标两个门槛都为 0 时拦截提交', async () => {
    await openModal({
      agent: agentVO({ auto_config: { auto_reply: autoReply() } })
    })
    form().auto_reply.comment_like_min = 0
    await submit()
    expect(mocks.updateAgent).not.toHaveBeenCalled()
    expect(bodyText()).toContain('回复评论时点赞数或回复数门槛至少一个 ≥ 1')
  })

  it('圈内创建：带 circle_id 与 auto_config，展示圈内说明', async () => {
    await openModal({ scope: 'circle', circleId: 'c1' })
    expect(bodyText()).toContain('圈内机器人只回复本圈内容')
    expect(bodyText()).toContain('机器人创建后默认启用，可在管理列表中停用。')
    Object.assign(form(), { name: 'cbot', model: 'm', trigger_keywords: ['k'] })
    await toggle(true)
    form().auto_reply.post_hot_min = 50
    await submit()
    expect(mocks.createCircleAgent).toHaveBeenCalledTimes(1)
    const payload = mocks.createCircleAgent.mock.calls[0][0]
    expect(payload.circle_id).toBe('c1')
    expect(payload).not.toHaveProperty('status')
    expect(mocks.updateCircleAgent).not.toHaveBeenCalled()
    expect(payload.auto_config).toEqual({
      auto_reply: {
        enabled: true,
        targets: ['post'],
        post_hot_min: 50,
        comment_like_min: 0,
        comment_reply_min: 0,
        window_hours: 24,
        max_per_hour: 0
      }
    })
  })

  it('圈内创建：未开启自动回复时不带 auto_config', async () => {
    await openModal({ scope: 'circle', circleId: 'c1' })
    Object.assign(form(), { name: 'cbot', model: 'm', trigger_keywords: ['k'] })
    await submit()
    expect(mocks.createCircleAgent.mock.calls[0][0]).not.toHaveProperty('auto_config')
  })

  it('圈内编辑（管理员）：可改自动回复，后端错误原样展示', async () => {
    mocks.updateCircleAgent.mockRejectedValue(new Error('auto_config.auto_reply is invalid'))
    await openModal({
      scope: 'circle',
      canEditCredentials: false,
      agent: agentVO({ auto_config: { auto_reply: autoReply() } })
    })
    form().auto_reply.window_hours = 48
    await submit()
    expect(mocks.updateCircleAgent).toHaveBeenCalledWith('a1', {
      auto_config: { auto_reply: autoReply({ window_hours: 48 }) }
    })
    expect(mocks.message.error).toHaveBeenCalledWith('auto_config.auto_reply is invalid')
  })
  const autoPost = () => ({
    enabled: true, interval_min: 60, daily_max: 3,
    active_hours: [22, 6], topics: ['游戏'], require_review: false
  })

  it('全局编辑：保留手动模式，只提交变更的完整 auto_post；关闭保留时段和关键词', async () => {
    await openModal({ agent: agentVO({
      trigger_mode: 3, trigger_keywords: [],
      auto_config: { auto_post: autoPost(), auto_reply: autoReply() }
    }) })
    expect(form().auto_post).toEqual(autoPost())
    expect(form().trigger_mode).toBe(3)
    const postSwitch = wrapper.findAllComponents(NSwitch)[1]
    expect(postSwitch.props('value')).toBe(true)
    postSwitch.vm.$emit('update:value', false)
    await submit()
    expect(mocks.updateAgent).toHaveBeenCalledExactlyOnceWith('a1', {
      auto_config: { auto_post: { ...autoPost(), enabled: false } }
    })
  })

  it('全局创建手动机器人：提示默认启用，单次创建提交完整的发帖配置', async () => {
    await openModal()
    expect(bodyText()).toContain('机器人创建后默认启用，可在管理列表中停用。')
    Object.assign(form(), { name: 'manual', model: 'model', trigger_mode: 3 })
    wrapper.findAllComponents(NSwitch)[1].vm.$emit('update:value', true)
    await flushPromises()
    expect(bodyText()).toContain('UTC+8')
    Object.assign(form().auto_post, { active_hours: [22, 6], daily_max: 3, topics: [' 游戏 ', '游戏'] })
    await submit()
    expect(mocks.createAgent).toHaveBeenCalledExactlyOnceWith(expect.objectContaining({
      trigger_mode: 3, auto_config: { auto_post: autoPost() }
    }))
    expect(mocks.createAgent.mock.calls[0][0]).not.toHaveProperty('status')
    expect(mocks.updateAgent).not.toHaveBeenCalled()
  })

  it('圈内管理员修改运营字段时跳过凭据和 auto_post，允许把关键词机器人成为手动模式', async () => {
    await openModal({ scope: 'circle', canEditCredentials: false, agent: agentVO() })
    expect(bodyText()).not.toContain('向热点圈自动发帖')
    Object.assign(form(), { trigger_mode: 3, base_url: 'changed', new_api_key: 'fixture', api_protocol: 'anthropic', auto_post: autoPost() })
    await submit()
    expect(mocks.updateCircleAgent).toHaveBeenCalledExactlyOnceWith('a1', { trigger_mode: 3 })
  })

  it('全局编辑换回关键词模式要求关键词；发帖时段错误阻止保存，修复后两个子对象同时提交', async () => {
    await openModal({ agent: agentVO({ trigger_mode: 3, trigger_keywords: [] }) })
    form().trigger_mode = 2
    await submit()
    expect(mocks.message.warning).toHaveBeenCalledWith('至少需要一个触发关键词')
    Object.assign(form(), { trigger_keywords: ['hi'], auto_reply: autoReply(), auto_post: { ...autoPost(), active_hours: [6, 6] } })
    await submit()
    expect(mocks.updateAgent).not.toHaveBeenCalled()
    expect(mocks.message.warning.mock.lastCall[0]).toContain('起止时间须不同')
    form().auto_post.active_hours = [22, 6]
    await submit()
    expect(mocks.updateAgent).toHaveBeenCalledExactlyOnceWith('a1', {
      trigger_mode: 2, trigger_keywords: ['hi'], auto_config: { auto_reply: autoReply(), auto_post: autoPost() }
    })
  })

})
