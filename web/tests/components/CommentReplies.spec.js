import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import CommentReplies from '@/components/post/detail/CommentReplies.vue'
import { createTestI18n, createTestRouter } from '../helpers/plugins'
import { __resetBreakpoint } from '@/composables/useBreakpoint'

const reply = (id, extra = {}) => ({ id, author_id: 'u-' + id, author_name: 'User ' + id, content: 'c-' + id, like_count: 0, liked: false, ...extra })

const makeData = (overrides = {}) => ({
  pages: [[reply('r1'), reply('r2')], [reply('r3')]],
  currentPage: 0,
  cursors: ['', 'c1', 'c2'],
  hasMoreMap: { 0: true, 1: false },
  loading: false,
  pageLoading: false,
  sortLoading: false,
  sort: 1,
  ...overrides
})

const mountReplies = (props = {}) => mount(CommentReplies, {
  props: { root: { id: 'c0', reply_count: 13 }, data: makeData(), postId: 'p1', ...props },
  global: {
    plugins: [createTestI18n(), createTestRouter([{ path: '/' }, { path: '/user/:id' }])],
    // 正文走 MdPreview，测试只关心列表结构
    stubs: {
      MentionPreview: { props: ['content'], template: '<span class="md">{{ content }}</span>' },
      ImageCarousel: true,
      CommentReplyEditor: { props: ['replyToId'], template: '<div class="reply-editor-stub" :data-to="replyToId" />' }
    }
  }
})

const replyIds = (wrapper) => wrapper.findAll('.reply-item .md').map(n => n.text().replace('c-', ''))

describe('CommentReplies', () => {
  beforeEach(() => __resetBreakpoint())

  it('inline：只展示当前页，带收起与翻页', async () => {
    const wrapper = mountReplies()
    expect(replyIds(wrapper)).toEqual(['r1', 'r2'])
    expect(wrapper.find('.collapse-replies-btn').exists()).toBe(true)
    expect(wrapper.find('.page-number').text()).toBe('1 / 2')
    expect(wrapper.find('.load-more-replies-btn').exists()).toBe(false)

    const [prev, next] = wrapper.findAll('.page-arrow-btn')
    expect(prev.attributes('disabled')).toBeDefined()
    await next.trigger('click')
    await wrapper.find('.collapse-replies-btn').trigger('click')
    expect(wrapper.emitted('next-page')).toHaveLength(1)
    expect(wrapper.emitted('collapse')).toHaveLength(1)
  })

  it('sheet：拼接全部已加载页，跳过定位留下的 null 页并按 id 去重', () => {
    const data = makeData({
      pages: [[reply('new'), reply('r5')], null, [reply('r5'), reply('r6')]],
      currentPage: 0,
      hasMoreMap: { 2: true }
    })
    const wrapper = mountReplies({ variant: 'sheet', data })
    expect(replyIds(wrapper)).toEqual(['new', 'r5', 'r6'])
    expect(wrapper.find('.collapse-replies-btn').exists()).toBe(false)
    expect(wrapper.find('.page-arrow-btn').exists()).toBe(false)
  })

  it('sheet：最后一个已加载页还有更多时显示「加载更多」并 emit load-more', async () => {
    const wrapper = mountReplies({ variant: 'sheet', data: makeData({ hasMoreMap: { 0: false, 1: true } }) })
    const btn = wrapper.find('.load-more-replies-btn')
    expect(btn.text()).toBe('加载更多回复')
    await btn.trigger('click')
    expect(wrapper.emitted('load-more')).toHaveLength(1)
  })

  it('sheet：全部加载完显示「没有更多了」；翻页加载中显示 spinner', () => {
    const done = mountReplies({ variant: 'sheet' })
    expect(done.find('.replies-no-more').text()).toBe('没有更多了')

    const loading = mountReplies({ variant: 'sheet', data: makeData({ pageLoading: true }) })
    expect(loading.find('.load-more-replies-btn').exists()).toBe(false)
    expect(loading.find('.replies-sheet-footer .n-spin-body').exists()).toBe(true)
  })

  it('排序切换：点非当前排序才 emit toggle-sort', async () => {
    const wrapper = mountReplies()
    const [newest, hottest] = wrapper.findAll('.sort-option')
    await newest.trigger('click')
    expect(wrapper.emitted('toggle-sort')).toBeUndefined()
    await hottest.trigger('click')
    expect(wrapper.emitted('toggle-sort')).toHaveLength(1)
  })

  it('回复按钮 emit open-reply；高亮目标回复', async () => {
    const wrapper = mountReplies({ highlightCommentId: 'r2' })
    const items = wrapper.findAll('.reply-item')
    expect(items[1].classes()).toContain('comment-highlight')
    expect(items[1].attributes('id')).toBe('comment-r2')
    const replyBtn = items[0].findAll('button').find(b => b.text().includes('回复'))
    await replyBtn.trigger('click')
    expect(wrapper.emitted('open-reply')[0][0].id).toBe('r1')
  })

  it('inline：activeReplyId 命中的回复下方内联桌面编辑器', () => {
    const wrapper = mountReplies({ activeReplyId: 'r2' })
    const editors = wrapper.findAll('.reply-editor-stub')
    expect(editors).toHaveLength(1)
    expect(editors[0].attributes('data-to')).toBe('r2')
  })

  it('sheet：不内联任何输入框（回复走底部输入抽屉），点回复只 emit open-reply', async () => {
    const wrapper = mountReplies({ variant: 'sheet', activeReplyId: 'r1' })
    expect(wrapper.find('.reply-editor-stub').exists()).toBe(false)
    expect(wrapper.find('textarea').exists()).toBe(false)
    const replyBtn = wrapper.findAll('.reply-item')[1].findAll('button').find(b => b.text().includes('回复'))
    await replyBtn.trigger('click')
    expect(wrapper.emitted('open-reply')[0][0].id).toBe('r2')
  })
})
