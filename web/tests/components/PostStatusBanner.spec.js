import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { NAlert } from 'naive-ui'
import PostStatusBanner from '@/components/post/detail/PostStatusBanner.vue'
import { createTestI18n } from '../helpers/plugins'

const mountBanner = (props) => mount(PostStatusBanner, {
  props,
  global: { plugins: [createTestI18n()] }
})

describe('PostStatusBanner（作者视角提示条）', () => {
  it('已发布：不渲染', () => {
    expect(mountBanner({ status: 1 }).findComponent(NAlert).exists()).toBe(false)
  })

  it('审核中：info + 刷新按钮', () => {
    const wrapper = mountBanner({ status: 2 })
    expect(wrapper.findComponent(NAlert).props('type')).toBe('info')
    expect(wrapper.find('.banner-text').text()).toBe('帖子正在审核中，仅你自己可见')
    expect(wrapper.find('.banner-refresh').exists()).toBe(true)
  })

  it('驳回（status=3）：warning，文案与「我的帖子」标签说明一致，无刷新按钮', () => {
    const wrapper = mountBanner({ status: 3 })
    expect(wrapper.findComponent(NAlert).props('type')).toBe('warning')
    expect(wrapper.find('.banner-text').text()).toBe('仅你自己可见。帖子未通过 AI 审核或被圈主 / 管理员驳回 / 下架，结果已通过消息中心通知你')
    expect(wrapper.find('.banner-refresh').exists()).toBe(false)
  })

  it('屏蔽（status=4）：info，等待复核说明，无刷新按钮', () => {
    const wrapper = mountBanner({ status: 4 })
    expect(wrapper.findComponent(NAlert).props('type')).toBe('info')
    expect(wrapper.find('.banner-text').text()).toBe('帖子被多次举报，已暂时对他人隐藏，等待圈主 / 管理员复核')
    expect(wrapper.find('.banner-refresh').exists()).toBe(false)
  })
})
