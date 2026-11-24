import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MobileComposeBar from '@/components/post/detail/MobileComposeBar.vue'
import { createTestI18n } from '../helpers/plugins'

const mountBar = (props = {}) => mount(MobileComposeBar, {
  props: { placeholder: '说点什么…', ...props },
  global: { plugins: [createTestI18n()] }
})

describe('MobileComposeBar', () => {
  it('点伪输入框 emit compose', async () => {
    const wrapper = mountBar()
    expect(wrapper.find('.compose-bar-placeholder').text()).toBe('说点什么…')
    await wrapper.find('.compose-bar-trigger').trigger('click')
    expect(wrapper.emitted('compose')).toHaveLength(1)
  })

  it('传 count 时显示评论数，点击 emit count-click', async () => {
    const wrapper = mountBar({ count: 12, fixed: true })
    expect(wrapper.classes()).toContain('is-fixed')
    const count = wrapper.find('.compose-bar-count')
    expect(count.text()).toBe('12')
    expect(count.attributes('aria-label')).toBe('查看评论')
    await count.trigger('click')
    expect(wrapper.emitted('count-click')).toHaveLength(1)
  })

  it('不传 count（回复抽屉内）：不显示评论数，不固定定位', () => {
    const wrapper = mountBar()
    expect(wrapper.find('.compose-bar-count').exists()).toBe(false)
    expect(wrapper.classes()).not.toContain('is-fixed')
  })
})
