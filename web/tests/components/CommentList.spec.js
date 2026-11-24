import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHistory } from 'vue-router'
import { NDrawer } from 'naive-ui'

const mocks = vi.hoisted(() => ({
  getCommentList: vi.fn(),
  getCommentReplies: vi.fn(),
  locateComment: vi.fn(),
  submit: vi.fn(),
  requireLogin: vi.fn(),
  message: { success: vi.fn(), error: vi.fn(), info: vi.fn() }
}))

vi.mock('@/api/comment', async (importOriginal) => ({
  ...(await importOriginal()),
  getCommentList: mocks.getCommentList,
  getCommentReplies: mocks.getCommentReplies,
  locateComment: mocks.locateComment
}))
vi.mock('@/api/like', () => ({ toggleLike: vi.fn() }))
vi.mock('@/composables/useCommentSubmit', async (importOriginal) => {
  const { ref } = await import('vue')
  return {
    ...(await importOriginal()),
    useCommentSubmit: () => ({ submitting: ref(false), submit: mocks.submit })
  }
})
vi.mock('@/utils/guest-action', () => ({ requireLogin: mocks.requireLogin }))
vi.mock('naive-ui', async (importOriginal) => ({
  ...(await importOriginal()),
  useMessage: () => mocks.message
}))

const { default: CommentList } = await import('@/components/post/detail/CommentList.vue')
const { createTestI18n } = await import('../helpers/plugins')
const { __resetBreakpoint } = await import('@/composables/useBreakpoint')
const { setMatchMedia } = await import('../setup')

const reply = (id) => ({ id, author_id: 'u-' + id, author_name: 'User ' + id, content: 'c-' + id, like_count: 0, liked: false })
const range = (from, to) => Array.from({ length: to - from }, (_, i) => reply('r' + (from + i)))
const rootComment = (id, replyCount) => ({ id, author_id: 'u-' + id, author_name: 'Root ' + id, content: 'c-' + id, like_count: 0, liked: false, reply_count: replyCount })

// 23 条回复，每页 10 条，游标为偏移量
const repliesApi = ({ root_id: rootId, cursor }) => {
  const all = rootId === 'c0' ? range(0, 23) : []
  const start = Number(cursor || 0)
  return Promise.resolve({ data: { items: all.slice(start, start + 10), has_more: start + 10 < all.length, next_cursor: String(start + 10) } })
}

let router
let wrapper

const Stub = { render: () => null }

// 用 web history（jsdom）：关闭抽屉要读 vue-router 写入 history.state 的 back 字段
const mountList = async ({ mobile = true, path = '/post/p1', history = [], props = {} } = {}) => {
  setMatchMedia(mobile)
  router = createRouter({
    history: createWebHistory(),
    routes: [{ path: '/post/:id', name: 'post', component: Stub }, { path: '/user/:id', component: Stub }, { path: '/', component: Stub }]
  })
  router.push('/')
  await router.isReady()
  for (const entry of [...history, path]) await router.push(entry)
  wrapper = mount(CommentList, {
    props: { postId: 'p1', commentCount: 2, ...props },
    attachTo: document.body,
    global: {
      plugins: [createTestI18n(), router],
      stubs: {
        MentionPreview: { props: ['content'], template: '<span class="md">{{ content }}</span>' },
        ImageCarousel: true,
        CommentReplyEditor: true
      }
    }
  })
  await flushPromises()
  return wrapper
}

// NDrawer 传送到 body：抽屉内容从 document 查
const sheet = () => document.querySelector('.reply-sheet')
const drawer = () => wrapper.findAllComponents(NDrawer).find(d => d.vm.$attrs.class === 'reply-sheet')
const composerDrawer = () => wrapper.findAllComponents(NDrawer).find(d => d.vm.$attrs.class === 'comment-composer')
const composerTitle = () => document.querySelector('.composer-title')?.textContent
const replyButton = (el) => [...el.querySelectorAll('button')].find(b => b.textContent.includes('回复'))
const mainReplyButton = (w, index) => replyButton(w.findAll('.comment-list > .comment-item')[index].element)
const login = () => {
  localStorage.setItem('qubar_token', 'tok')
  localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
}
// 在输入抽屉里输入并发送
const sendFromComposer = async (text) => {
  const textarea = document.querySelector('.comment-composer textarea')
  textarea.value = text
  textarea.dispatchEvent(new Event('input'))
  await flushPromises()
  ;[...document.querySelectorAll('.comment-composer button')].find(b => b.textContent.includes('发送')).click()
  await settle()
}
const sheetReplyIds = () => [...document.querySelectorAll('.reply-sheet .reply-item .md')].map(n => n.textContent.replace('c-', ''))
const viewRepliesBtn = (w, index = 0) => w.findAll('.load-replies-btn')[index]
// jsdom 的 history.back() 异步派发 popstate：多等一轮宏任务让路由落定
const settle = async () => {
  await flushPromises()
  await new Promise(r => setTimeout(r, 20))
  await flushPromises()
}

describe('CommentList 移动端回复抽屉', () => {
  beforeEach(() => {
    __resetBreakpoint()
    // jsdom 未实现：懒加载 observer 直接视为进入视口；scrollIntoView 记录调用
    vi.stubGlobal('IntersectionObserver', class {
      constructor(cb) { this.cb = cb }
      observe(el) { this.cb([{ isIntersecting: true, target: el }]) }
      disconnect() {}
    })
    Element.prototype.scrollIntoView = vi.fn()
    mocks.getCommentList.mockReset().mockResolvedValue({ data: { items: [rootComment('c0', 23), rootComment('c1', 0)], has_more: false, next_cursor: '' } })
    mocks.getCommentReplies.mockReset().mockImplementation(repliesApi)
    mocks.locateComment.mockReset()
    mocks.submit.mockReset()
    mocks.requireLogin.mockReset()
  })

  afterEach(() => {
    wrapper?.unmount()
    document.body.innerHTML = ''
  })

  it('点「查看回复」打开抽屉：push ?replies=，抽屉内展示根评论与回复，主列表不内联展开', async () => {
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()

    expect(router.currentRoute.value.query.replies).toBe('c0')
    expect(mocks.getCommentReplies).toHaveBeenCalledWith({ root_id: 'c0', sort: 1 })
    expect(sheet()).not.toBeNull()
    expect(document.querySelector('.reply-sheet-title').textContent).toBe('23 条回复')
    expect(sheetReplyIds()).toEqual(range(0, 10).map(r => r.id))
    // 抽屉里的根评论不占用定位锚点
    expect(document.querySelectorAll('#comment-c0')).toHaveLength(1)
    expect(w.find('.comment-list .comment-replies').exists()).toBe(false)
    // 移动端「查看回复」按钮常驻
    expect(viewRepliesBtn(w).exists()).toBe(true)
  })

  it('加载更多：向下追加下一页，直到没有更多', async () => {
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()

    document.querySelector('.load-more-replies-btn').click()
    await settle()
    expect(mocks.getCommentReplies).toHaveBeenLastCalledWith({ root_id: 'c0', sort: 1, cursor: '10' })
    expect(sheetReplyIds()).toHaveLength(20)

    document.querySelector('.load-more-replies-btn').click()
    await settle()
    expect(sheetReplyIds()).toEqual(range(0, 23).map(r => r.id))
    expect(document.querySelector('.load-more-replies-btn')).toBeNull()
  })

  it('返回（手势 / 后退）关闭抽屉，留在帖子页；再次打开复用缓存', async () => {
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()

    router.back()
    await settle()
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(drawer().props('show')).toBe(false)

    await viewRepliesBtn(w).trigger('click')
    await settle()
    expect(sheetReplyIds()).toHaveLength(10)
    expect(mocks.getCommentReplies).toHaveBeenCalledTimes(1)
  })

  it('关闭按钮走 router.back，重复点击不会多退一步', async () => {
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()
    const back = vi.spyOn(router, 'back')

    const close = document.querySelector('.reply-sheet-close')
    close.click()
    close.click()
    await settle()
    expect(back).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
  })

  it('主列表点「回复」：打开输入抽屉（不内联）；回复未加载过的根评论只加回复数，打开抽屉时完整拉取', async () => {
    login()
    mocks.submit.mockResolvedValue(reply('mine'))
    const w = await mountList()
    mainReplyButton(w, 0).click()
    await settle()
    expect(router.currentRoute.value.query).toEqual({ compose: 'c0' })
    expect(composerTitle()).toBe('回复 @Root c0')
    expect(w.find('.comment-list textarea').exists()).toBe(false)

    await sendFromComposer('hi')
    expect(mocks.submit).toHaveBeenCalledWith({ postId: 'p1', content: 'hi', reply: { rootId: 'c0', replyToId: null, replyToName: 'Root c0' } })
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(viewRepliesBtn(w).text()).toContain('24')

    await viewRepliesBtn(w).trigger('click')
    await settle()
    expect(mocks.getCommentReplies).toHaveBeenCalledTimes(1)
    expect(sheetReplyIds()[0]).toBe('r0')
  })

  it('抽屉内回复某条回复：带 reply-to-id，新回复插到顶部并高亮，离场动画结束后滚动过去', async () => {
    login()
    mocks.submit.mockResolvedValue(reply('mine'))
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()

    replyButton(document.querySelector('.reply-sheet .reply-item')).click()
    await settle()
    expect(router.currentRoute.value.query).toEqual({ replies: 'c0', compose: 'r0' })
    expect(composerTitle()).toBe('回复 @User r0')

    await sendFromComposer('yo')
    expect(mocks.submit).toHaveBeenCalledWith({ postId: 'p1', content: 'yo', reply: { rootId: 'c0', replyToId: 'r0', replyToName: 'User r0' } })
    expect(router.currentRoute.value.query).toEqual({ replies: 'c0' })
    expect(sheetReplyIds().slice(0, 2)).toEqual(['mine', 'r0'])
    expect(document.querySelector('.reply-sheet-title').textContent).toBe('24 条回复')
    expect(document.getElementById('comment-mine').classList).toContain('comment-highlight')

    Element.prototype.scrollIntoView.mockClear()
    composerDrawer().props('onAfterLeave')()
    expect(Element.prototype.scrollIntoView.mock.contexts.map(el => el.id)).toEqual(['comment-mine'])
  })

  it('顶层评论（openComposer）：插到列表顶部、评论数 +1 并高亮', async () => {
    login()
    mocks.submit.mockResolvedValue(rootComment('new', 0))
    const w = await mountList()
    w.vm.openComposer({ rootId: null, replyToId: null, replyToName: '' })
    await settle()
    expect(router.currentRoute.value.query).toEqual({ compose: 'post' })
    expect(composerTitle()).toBe('发表评论')

    await sendFromComposer('top')
    expect(mocks.submit).toHaveBeenCalledWith({ postId: 'p1', content: 'top', reply: null })
    expect(w.emitted('update:commentCount')[0]).toEqual([3])
    expect(w.findAll('.comment-list > .comment-item')[0].attributes('id')).toBe('comment-new')
    expect(w.find('#comment-new').classes()).toContain('comment-highlight')
  })

  it('通知定位到回复：主列表滚到根评论，打开抽屉并高亮目标', async () => {
    mocks.locateComment.mockResolvedValue({
      data: { comment_id: 'r15', post_id: 'p1', root_id: 'c0', is_root: false, list_cursor: '', reply_cursor: '10', reply_page: 2 }
    })
    await mountList({ path: '/post/p1?comment_id=r15', props: { locateCommentId: 'r15' } })
    await settle()

    expect(router.currentRoute.value.query).toEqual({ comment_id: 'r15', replies: 'c0' })
    expect(sheetReplyIds()).toEqual(range(10, 20).map(r => r.id))
    expect(document.getElementById('comment-r15').classList).toContain('comment-highlight')
    const scrolled = Element.prototype.scrollIntoView.mock.contexts.map(el => el.id)
    expect(scrolled[0]).toBe('comment-c0')
  })

  it('挂载时残留的 ?replies= 被去掉：上一条是同页时后退，不留重复记录', async () => {
    await mountList({ history: ['/post/p1'], path: '/post/p1?replies=c0' })
    await settle()
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(window.history.state.back).toBe('/')
    expect(sheet()).toBeNull()
  })

  it('挂载时残留的 ?replies= 且上一条是别的页：replace 去掉，不退出帖子', async () => {
    await mountList({ history: ['/user/u1'], path: '/post/p1?replies=c0' })
    await settle()
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(window.history.state.back).toBe('/user/u1')
  })

  it('定位抽屉滚到目标回复：进场动画结束（after-enter）后滚动', async () => {
    mocks.locateComment.mockResolvedValue({
      data: { comment_id: 'r15', post_id: 'p1', root_id: 'c0', is_root: false, list_cursor: '', reply_cursor: '10', reply_page: 2 }
    })
    await mountList({ path: '/post/p1?comment_id=r15', props: { locateCommentId: 'r15' } })
    await settle()
    Element.prototype.scrollIntoView.mockClear()

    drawer().props('onAfterEnter')()
    expect(Element.prototype.scrollIntoView.mock.contexts.map(el => el.id)).toEqual(['comment-r15'])
  })

  it('定位后关闭再打开：丢弃跳页数据，从第一页重新加载', async () => {
    mocks.locateComment.mockResolvedValue({
      data: { comment_id: 'r15', post_id: 'p1', root_id: 'c0', is_root: false, list_cursor: '', reply_cursor: '10', reply_page: 2 }
    })
    const w = await mountList({ path: '/post/p1?comment_id=r15', props: { locateCommentId: 'r15' } })
    await settle()
    router.back()
    await settle()
    expect(router.currentRoute.value.query).toEqual({ comment_id: 'r15' })

    await viewRepliesBtn(w).trigger('click')
    await settle()
    expect(mocks.getCommentReplies).toHaveBeenLastCalledWith({ root_id: 'c0', sort: 1 })
    expect(sheetReplyIds()).toEqual(range(0, 10).map(r => r.id))
    // 关闭时清掉了定位高亮，再开不重复闪烁
    expect(document.querySelector('.reply-sheet .comment-highlight')).toBeNull()
  })

  it('点遮罩（update:show=false）关闭抽屉', async () => {
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()

    drawer().props('onUpdate:show')(false)
    await settle()
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(drawer().props('show')).toBe(false)
  })

  it('关闭后抽屉内容保留到离场动画结束，避免空白面板滑出', async () => {
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()
    router.back()
    await settle()

    expect(drawer().props('show')).toBe(false)
    expect(w.vm.$.setupState.sheetDisplayComment?.id).toBe('c0')
    drawer().props('onAfterLeave')()
    await settle()
    expect(w.vm.$.setupState.sheetDisplayComment).toBeNull()
  })

  it('跨到桌面断点：关闭抽屉并去掉 query，回复按第一页内联展示', async () => {
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()
    document.querySelector('.load-more-replies-btn').click()
    await settle()

    setMatchMedia(false)
    await settle()
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(sheet()).toBeNull()
    expect(w.find('.page-number').text()).toBe('1 / 3')
  })

  it('回复抽屉底部输入条：打开回复根评论的输入抽屉，叠在回复抽屉上方', async () => {
    login()
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()

    const bar = document.querySelector('.reply-sheet .n-drawer-footer .compose-bar-trigger')
    expect(bar.textContent.trim()).toBe('回复 @Root c0')
    bar.click()
    await settle()
    expect(router.currentRoute.value.query).toEqual({ replies: 'c0', compose: 'c0' })
    expect(composerDrawer().props('show')).toBe(true)
    expect(drawer().props('show')).toBe(true)
    // 抽屉里不再内联输入框
    expect(document.querySelector('.reply-sheet textarea')).toBeNull()
  })

  it('返回手势：先关输入抽屉，再关回复抽屉，最后留在帖子页', async () => {
    login()
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()
    replyButton(document.querySelector('.reply-sheet .reply-item')).click()
    await settle()

    router.back()
    await settle()
    expect(router.currentRoute.value.query).toEqual({ replies: 'c0' })
    expect(composerDrawer().props('show')).toBe(false)
    expect(drawer().props('show')).toBe(true)

    router.back()
    await settle()
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(drawer().props('show')).toBe(false)
  })

  it('访客点「回复」：弹登录引导，不打开输入抽屉', async () => {
    const w = await mountList()
    mainReplyButton(w, 0).click()
    await settle()
    expect(mocks.requireLogin).toHaveBeenCalledWith('comment')
    expect(router.currentRoute.value.query).toEqual({})
  })

  it('挂载时残留的 ?replies= 与 ?compose=：一并去掉，上一条是同页时后退', async () => {
    await mountList({ history: ['/post/p1'], path: '/post/p1?replies=c0&compose=r1' })
    await settle()
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(window.history.state.back).toBe('/')
    expect(sheet()).toBeNull()
  })

  it('输入抽屉开着时跨到桌面断点：去掉 query，回复改回内联编辑器', async () => {
    login()
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()
    document.querySelector('.reply-sheet .compose-bar-trigger').click()
    await settle()

    setMatchMedia(false)
    await settle()
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(composerDrawer()).toBeUndefined()

    mainReplyButton(w, 0).click()
    await settle()
    expect(w.findComponent({ name: 'CommentReplyEditor' }).exists()).toBe(true)
    expect(router.currentRoute.value.query).toEqual({})
  })

  it('首次加载失败：显示重试，点击后重新拉取', async () => {
    mocks.getCommentReplies.mockRejectedValueOnce(new Error('network'))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const w = await mountList()
    await viewRepliesBtn(w).trigger('click')
    await settle()

    const retry = document.querySelector('.load-more-replies-btn')
    expect(retry.textContent.trim()).toBe('加载失败，点击重试')
    retry.click()
    await settle()
    expect(sheetReplyIds()).toHaveLength(10)
  })

  it('帖子不可见（私密圈 404）：评论列表停止翻页，不反复重试（后端 #170）', async () => {
    mocks.getCommentList.mockReset().mockRejectedValue(Object.assign(new Error('not found'), { code: 204, status: 404 }))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    await mountList()
    await settle()
    expect(mocks.getCommentList).toHaveBeenCalledTimes(1)
    expect(mocks.message.error).not.toHaveBeenCalled()
  })

  it('通知定位：评论所属帖子不可见（HTTP 404）时提示内容已删除，而不是定位失败', async () => {
    mocks.locateComment.mockRejectedValue(Object.assign(new Error('not found'), { code: 204, status: 404 }))
    mocks.message.info.mockReset()
    mocks.message.error.mockReset()
    vi.spyOn(console, 'error').mockImplementation(() => {})
    await mountList({ props: { locateCommentId: 'x1' } })
    await settle()
    expect(mocks.message.info).toHaveBeenCalledWith('内容已被删除')
    expect(mocks.message.error).not.toHaveBeenCalled()
  })
})

describe('CommentList 桌面端回复', () => {
  beforeEach(() => {
    __resetBreakpoint()
    vi.stubGlobal('IntersectionObserver', class {
      constructor(cb) { this.cb = cb }
      observe(el) { this.cb([{ isIntersecting: true, target: el }]) }
      disconnect() {}
    })
    mocks.getCommentList.mockReset().mockResolvedValue({ data: { items: [rootComment('c0', 23)], has_more: false, next_cursor: '' } })
    mocks.getCommentReplies.mockReset().mockImplementation(repliesApi)
  })

  afterEach(() => {
    wrapper?.unmount()
    document.body.innerHTML = ''
  })

  it('内联展开 / 翻页 / 收起，不改路由、不渲染抽屉', async () => {
    const w = await mountList({ mobile: false })
    await viewRepliesBtn(w).trigger('click')
    await settle()

    expect(router.currentRoute.value.query).toEqual({})
    expect(sheet()).toBeNull()
    // 移动端输入抽屉与代理输入框不渲染
    expect(document.querySelector('.composer-focus-proxy')).toBeNull()
    expect(w.findAll('.comment-replies .reply-item')).toHaveLength(10)
    expect(viewRepliesBtn(w)).toBeUndefined()

    await w.findAll('.page-arrow-btn')[1].trigger('click')
    await settle()
    expect(w.find('.page-number').text()).toBe('2 / 3')
    expect(w.findAll('.comment-replies .reply-item .md')[0].text()).toBe('c-r10')

    await w.find('.collapse-replies-btn').trigger('click')
    expect(w.find('.comment-replies').exists()).toBe(false)
    expect(viewRepliesBtn(w).exists()).toBe(true)
  })
})
