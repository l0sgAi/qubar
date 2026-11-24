import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'

// 全文 Markdown 渲染与本组件逻辑无关，替换为占位
vi.mock('@/components/post/detail/MentionPreview.vue', () => ({
  default: { props: ['content'], template: '<div class="md-stub">{{ content }}</div>' }
}))
import ModerationPostItem from '@/components/circle/ModerationPostItem.vue'
import { createTestI18n, createTestRouter } from '../helpers/plugins'

const routes = [{ path: '/' }, { path: '/user/:id' }]

const basePost = {
  id: 'p1',
  user_id: 'u1',
  title: '标题',
  summary: '摘要',
  content: '完整正文',
  media_extra: [],
  create_time: new Date().toISOString(),
  author_name: '作者',
  author_avatar: '',
  ai_verdict: null
}

const mountItem = (post = {}, slots = {}) => mount(ModerationPostItem, {
  props: { post: { ...basePost, ...post } },
  slots,
  global: { plugins: [createTestI18n(), createTestRouter(routes)] }
})

const readyVerdict = {
  decision: 'review',
  hit_hazards: ['self_harm', 'spam_ad'],
  hazards: {
    sexual: 0.01, violence_terror: 0.02, harassment: 0.03, illegal: 0.01,
    spam_ad: 0.41, privacy: 0.01, self_harm: 0.91, manipulation: 0.02
  },
  severity: 2.1,
  severity_confidence: 0.86,
  model: 'jev-1.13.0',
  error_msg: null
}

describe('ModerationPostItem', () => {
  it('显示作者、标题、摘要；全文默认折叠，点击展开', async () => {
    const wrapper = mountItem()
    expect(wrapper.find('.mod-author-name').text()).toBe('作者')
    expect(wrapper.find('.mod-author-name').attributes('href')).toBe('/user/u1')
    expect(wrapper.find('.mod-title').text()).toBe('标题')
    expect(wrapper.find('.mod-summary').text()).toBe('摘要')
    expect(wrapper.find('.md-stub').exists()).toBe(false)
    await wrapper.find('.mod-content-wrap .mod-toggle').trigger('click')
    expect(wrapper.find('.md-stub').text()).toBe('完整正文')
  })

  it('ai_verdict 为 null：提示可直接人工处理', () => {
    const wrapper = mountItem()
    expect(wrapper.find('.ai-note').text()).toBe('暂无 AI 结论，可直接人工处理')
    expect(wrapper.find('.ai-care').exists()).toBe(false)
  })

  it('error_msg 非空：提示 AI 未能给出结论', () => {
    const wrapper = mountItem({
      ai_verdict: { decision: 'review', hit_hazards: [], hazards: {}, severity: null, error_msg: 'upstream timeout' }
    })
    expect(wrapper.find('.ai-note--warn').text()).toBe('AI 未能给出结论')
    expect(wrapper.text()).not.toContain('upstream timeout')
  })

  it('正常结论：命中标签带百分比，危害等级与置信度，命中 self_harm 提示关怀，全量表默认折叠', async () => {
    const wrapper = mountItem({ ai_verdict: readyVerdict })
    const tags = wrapper.findAll('.ai-tags .n-tag').map(w => w.text())
    expect(tags).toEqual(['自伤轻生 91%', '广告引流 41%'])
    expect(wrapper.find('.ai-meta').text()).toContain('严重（2.1）')
    expect(wrapper.find('.ai-meta').text()).toContain('置信度 86%')
    expect(wrapper.find('.ai-care').text()).toBe('可能需要人工关怀')
    expect(wrapper.find('.hazard-table').exists()).toBe(false)
    await wrapper.find('.ai-panel .mod-toggle').trigger('click')
    expect(wrapper.findAll('.hazard-row')).toHaveLength(8)
    expect(wrapper.findAll('.hazard-row--hit')).toHaveLength(2)
  })

  it('actions / extra 插槽拿到 post', () => {
    const wrapper = mountItem({}, {
      actions: '<template #actions="{ post }"><button class="act">{{ post.id }}</button></template>',
      extra: '<template #extra="{ post }"><div class="extra">{{ post.title }}</div></template>'
    })
    expect(wrapper.find('.mod-actions .act').text()).toBe('p1')
    expect(wrapper.find('.extra').text()).toBe('标题')
  })

  it('decision 标签：review_publish / fail_open 显示文案（fail_open 带 error_msg 也显示），review 不显示', () => {
    expect(mountItem({ ai_verdict: { ...readyVerdict, decision: 'review_publish' } }).find('.ai-decision').text())
      .toBe('AI 判断存疑，已自动发布')
    const failOpen = mountItem({
      ai_verdict: { decision: 'fail_open', hit_hazards: [], hazards: {}, severity: null, error_msg: 'timeout' }
    })
    expect(failOpen.find('.ai-decision').text()).toBe('AI 不可用，已直接发布')
    expect(failOpen.find('.ai-note--warn').exists()).toBe(true)
    expect(mountItem({ ai_verdict: readyVerdict }).find('.ai-decision').exists()).toBe(false)
    expect(mountItem().find('.ai-decision').exists()).toBe(false)
  })
})
