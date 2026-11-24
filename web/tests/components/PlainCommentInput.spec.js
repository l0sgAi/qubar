import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const mocks = vi.hoisted(() => ({
  submit: vi.fn(),
  message: { success: vi.fn(), error: vi.fn() }
}))

vi.mock('@/composables/useCommentSubmit', async (importOriginal) => {
  const { ref } = await import('vue')
  return {
    ...(await importOriginal()),
    useCommentSubmit: () => ({ submitting: ref(false), submit: mocks.submit })
  }
})
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => mocks.message
}))

const { default: PlainCommentInput } = await import('@/components/post/detail/PlainCommentInput.vue')
const { createTestI18n } = await import('../helpers/plugins')

let wrapper

// 内容由父组件 v-model:value 持有：update:value 回写到 props
const mountInput = (props = {}) => {
  wrapper = mount(PlainCommentInput, {
    props: {
      postId: 'p1',
      value: '',
      'onUpdate:value': (value) => wrapper.setProps({ value }),
      ...props
    },
    attachTo: document.body,
    global: { plugins: [createTestI18n()] }
  })
  return wrapper
}

const sendButton = (wrapper) => wrapper.findAll('button').find(b => b.text().includes('发送'))

describe('PlainCommentInput', () => {
  beforeEach(() => {
    mocks.submit.mockReset()
    mocks.message.success.mockReset()
    mocks.message.error.mockReset()
  })

  // 卸载以停掉 autosize 的 ResizeObserver，避免环境销毁后回调报错
  afterEach(() => {
    wrapper?.unmount()
    document.body.innerHTML = ''
  })

  it('内容为空时发送按钮禁用，显示纯文本提示', () => {
    const wrapper = mountInput()
    expect(sendButton(wrapper).attributes('disabled')).toBeDefined()
    expect(wrapper.find('.rich-hint').text()).toBe('图片和 @ 提及请在电脑端使用')
  })

  it('受控：显示父组件传入的草稿，输入回写 update:value', async () => {
    const wrapper = mountInput({ value: '草稿' })
    expect(wrapper.find('textarea').element.value).toBe('草稿')
    await wrapper.find('textarea').setValue('草稿2')
    expect(wrapper.emitted('update:value').at(-1)).toEqual(['草稿2'])
    expect(sendButton(wrapper).attributes('disabled')).toBeUndefined()
  })

  it('顶层评论：提交后提示、清空并 emit submit', async () => {
    mocks.submit.mockResolvedValue({ id: 'c1' })
    const wrapper = mountInput()
    await wrapper.find('textarea').setValue('第一行\n第二行')
    await sendButton(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.submit).toHaveBeenCalledWith({ postId: 'p1', content: '第一行\n第二行', reply: null })
    expect(mocks.message.success).toHaveBeenCalledWith('评论成功')
    expect(wrapper.emitted('submit')[0]).toEqual([{ id: 'c1' }])
    expect(wrapper.emitted('update:value').at(-1)).toEqual([''])
    expect(wrapper.find('textarea').element.value).toBe('')
  })

  it('回复模式：带 reply 参数，占位符含对方名字', async () => {
    mocks.submit.mockResolvedValue({ id: 'r1' })
    const wrapper = mountInput({ rootId: 'c1', replyToId: 'r0', replyToName: 'Bob' })
    expect(wrapper.find('textarea').attributes('placeholder')).toBe('回复 Bob')
    await wrapper.find('textarea').setValue('yo')
    await sendButton(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.submit).toHaveBeenCalledWith({
      postId: 'p1',
      content: 'yo',
      reply: { rootId: 'c1', replyToId: 'r0', replyToName: 'Bob' }
    })
    expect(mocks.message.success).toHaveBeenCalledWith('回复成功')
  })

  it('失败：提示错误，不清空、不 emit submit', async () => {
    mocks.submit.mockRejectedValue(new Error('服务异常'))
    const wrapper = mountInput()
    await wrapper.find('textarea').setValue('hi')
    await sendButton(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.message.error).toHaveBeenCalledWith('服务异常')
    expect(wrapper.emitted('submit')).toBeUndefined()
    expect(wrapper.find('textarea').element.value).toBe('hi')
  })

  it('只有空白时不提交', async () => {
    const wrapper = mountInput()
    await wrapper.find('textarea').setValue('   ')
    expect(sendButton(wrapper).attributes('disabled')).toBeDefined()
    await sendButton(wrapper).trigger('click')
    expect(mocks.submit).not.toHaveBeenCalled()
  })

  it('接近 2000 字上限才显示字数，到上限标红', async () => {
    const wrapper = mountInput({ value: 'a'.repeat(1799) })
    expect(wrapper.find('.char-count').exists()).toBe(false)
    await wrapper.setProps({ value: 'a'.repeat(1800) })
    expect(wrapper.find('.char-count').text()).toBe('1800/2000')
    expect(wrapper.find('.char-count').classes()).not.toContain('is-full')
    await wrapper.setProps({ value: 'a'.repeat(2000) })
    expect(wrapper.find('.char-count').classes()).toContain('is-full')
    expect(wrapper.find('textarea').attributes('maxlength')).toBe('2000')
  })

  it('expose focus / blur', () => {
    const wrapper = mountInput()
    wrapper.vm.focus()
    expect(document.activeElement).toBe(wrapper.find('textarea').element)
    wrapper.vm.blur()
    expect(document.activeElement).not.toBe(wrapper.find('textarea').element)
  })
})
