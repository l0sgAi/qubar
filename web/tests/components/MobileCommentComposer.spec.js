import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createWebHistory } from 'vue-router'
import { NDrawer } from 'naive-ui'

const mocks = vi.hoisted(() => ({
  submit: vi.fn(),
  requireLogin: vi.fn(),
  message: { success: vi.fn(), error: vi.fn() }
}))

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

const { default: MobileCommentComposer } = await import('@/components/post/detail/MobileCommentComposer.vue')
const { createTestI18n } = await import('../helpers/plugins')

const Stub = { render: () => null }
const TOP = { rootId: null, replyToId: null, replyToName: '' }
const REPLY_ROOT = { rootId: 'c1', replyToId: null, replyToName: 'Bob' }
const REPLY_REPLY = { rootId: 'c1', replyToId: 'r1', replyToName: 'Amy' }

let router
let wrapper

const login = () => {
  localStorage.setItem('qubar_token', 'tok')
  localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
}

// 用 web history（jsdom）：关闭要读 vue-router 写入 history.state 的 back 字段
const mountComposer = async ({ path = '/post/p1' } = {}) => {
  router = createRouter({
    history: createWebHistory(),
    routes: [{ path: '/post/:id', component: Stub }, { path: '/', component: Stub }]
  })
  router.push('/')
  await router.isReady()
  await router.push(path)
  wrapper = mount(MobileCommentComposer, {
    props: { postId: 'p1' },
    attachTo: document.body,
    global: { plugins: [createTestI18n(), router] }
  })
  await flushPromises()
  return wrapper
}

// jsdom 的 history.back() 异步派发 popstate：多等一轮宏任务让路由落定
const settle = async () => {
  await flushPromises()
  await new Promise(r => setTimeout(r, 20))
  await flushPromises()
}

const drawer = () => wrapper.findComponent(NDrawer)
const textarea = () => document.querySelector('.comment-composer textarea')
const proxy = () => document.querySelector('.composer-focus-proxy')
const query = () => router.currentRoute.value.query
const typeDraft = async (value) => {
  textarea().value = value
  textarea().dispatchEvent(new Event('input'))
  await flushPromises()
}
const sendButton = () => [...document.querySelectorAll('.comment-composer button')].find(b => b.textContent.includes('发送'))

const openAndSettle = async (target) => {
  wrapper.vm.open(target)
  await settle()
}

describe('MobileCommentComposer', () => {
  beforeEach(() => {
    mocks.submit.mockReset()
    mocks.requireLogin.mockReset()
    mocks.message.success.mockReset()
    mocks.message.error.mockReset()
  })

  afterEach(() => {
    wrapper?.unmount()
    document.body.innerHTML = ''
    delete window.visualViewport
  })

  it('访客：弹登录引导，不打开抽屉、不改路由', async () => {
    await mountComposer()
    await openAndSettle(TOP)
    expect(mocks.requireLogin).toHaveBeenCalledWith('comment')
    expect(query()).toEqual({})
    expect(drawer().props('show')).toBe(false)
  })

  it('打开：同步聚焦代理输入框（iOS 弹键盘），push ?compose=，渲染后焦点移到输入框', async () => {
    login()
    await mountComposer()
    wrapper.vm.open(TOP)
    // 点击处理里同步完成，不等路由
    expect(document.activeElement).toBe(proxy())

    await settle()
    expect(query()).toEqual({ compose: 'post' })
    expect(drawer().props('show')).toBe(true)
    expect(document.querySelector('.composer-title').textContent).toBe('发表评论')
    expect(textarea().placeholder).toBe('写下你的评论...')
    expect(document.activeElement).toBe(textarea())
  })

  it('回复根评论 / 回复某条回复：标题「回复 @名字」，query 为被回复的评论 id', async () => {
    login()
    await mountComposer()
    await openAndSettle(REPLY_ROOT)
    expect(query()).toEqual({ compose: 'c1' })
    expect(document.querySelector('.composer-title').textContent).toBe('回复 @Bob')
    expect(textarea().placeholder).toBe('回复 Bob')

    router.back()
    await settle()
    await openAndSettle(REPLY_REPLY)
    expect(query()).toEqual({ compose: 'r1' })
    expect(document.querySelector('.composer-title').textContent).toBe('回复 @Amy')
  })

  it('草稿：按目标保留，关闭再打开同一目标恢复，其它目标互不影响', async () => {
    login()
    await mountComposer()
    await openAndSettle(TOP)
    await typeDraft('顶层草稿')
    router.back()
    await settle()
    expect(query()).toEqual({})
    expect(drawer().props('show')).toBe(false)

    await openAndSettle(REPLY_ROOT)
    expect(textarea().value).toBe('')
    await typeDraft('回复草稿')
    router.back()
    await settle()

    await openAndSettle(TOP)
    expect(textarea().value).toBe('顶层草稿')
    router.back()
    await settle()
    await openAndSettle(REPLY_ROOT)
    expect(textarea().value).toBe('回复草稿')
  })

  it('关闭按钮 / 遮罩走 router.back，重复关闭不会多退一步', async () => {
    login()
    await mountComposer()
    await openAndSettle(TOP)
    const back = vi.spyOn(router, 'back')

    document.querySelector('.composer-close').click()
    drawer().props('onUpdate:show')(false)
    await settle()
    expect(back).toHaveBeenCalledTimes(1)
    expect(router.currentRoute.value.fullPath).toBe('/post/p1')
    expect(document.activeElement).not.toBe(textarea())
  })

  it('叠在回复抽屉上：保留 ?replies=，返回只关输入抽屉', async () => {
    login()
    await mountComposer({ path: '/post/p1?replies=c1' })
    await openAndSettle(REPLY_ROOT)
    expect(query()).toEqual({ replies: 'c1', compose: 'c1' })

    router.back()
    await settle()
    expect(query()).toEqual({ replies: 'c1' })
  })

  it('发送成功：emit submit(新评论, 目标)，收起抽屉并清空草稿；离场后 emit closed', async () => {
    login()
    mocks.submit.mockResolvedValue({ id: 'new' })
    await mountComposer()
    await openAndSettle(REPLY_REPLY)
    await typeDraft('hi')
    sendButton().click()
    await settle()

    expect(mocks.submit).toHaveBeenCalledWith({
      postId: 'p1',
      content: 'hi',
      reply: { rootId: 'c1', replyToId: 'r1', replyToName: 'Amy' }
    })
    expect(wrapper.emitted('submit')[0]).toEqual([{ id: 'new' }, REPLY_REPLY])
    expect(query()).toEqual({})
    expect(drawer().props('show')).toBe(false)

    drawer().props('onAfterLeave')()
    expect(wrapper.emitted('closed')).toHaveLength(1)

    await openAndSettle(REPLY_REPLY)
    expect(textarea().value).toBe('')
  })

  it('发送失败：抽屉和内容保留，提示错误', async () => {
    login()
    mocks.submit.mockRejectedValue(new Error('服务异常'))
    await mountComposer()
    await openAndSettle(TOP)
    await typeDraft('hi')
    sendButton().click()
    await settle()

    expect(mocks.message.error).toHaveBeenCalledWith('服务异常')
    expect(wrapper.emitted('submit')).toBeUndefined()
    expect(query()).toEqual({ compose: 'post' })
    expect(textarea().value).toBe('hi')
  })

  it('键盘弹出：抽屉底边抬到键盘上方', async () => {
    login()
    const viewport = new EventTarget()
    Object.assign(viewport, { height: 800, offsetTop: 0 })
    window.visualViewport = viewport
    window.innerHeight = 800
    await mountComposer()
    await openAndSettle(TOP)
    const panel = document.querySelector('.n-drawer.comment-composer')
    expect(panel.style.bottom).toBe('0px')

    viewport.height = 460
    viewport.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(panel.style.bottom).toBe('340px')
    expect(document.querySelector('.composer').classList).toContain('keyboard-open')
  })
})
