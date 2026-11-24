import { describe, it, expect, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'

// 登录页被路由表静态导入，这里替换为空组件，避免加载整页依赖
vi.mock('@/views/auth/Login.vue', () => ({ default: { render: () => null } }))

const { routes, authGuard } = await import('@/router')

const Stub = { render: () => null }

// 用导出的路由表 + 守卫组装独立 router：保留 path / name / meta，组件替换为 stub
const setup = async (start = '/home') => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: routes.map(r => ({ ...r, component: Stub }))
  })
  router.beforeEach(authGuard)
  await router.push(start)
  return router
}

const login = (expireInMs = 60_000) => {
  localStorage.setItem('qubar_token', 'tok')
  localStorage.setItem('qubar_token_expire', String(Date.now() + expireInMs))
}

const PROTECTED = [
  '/create-post',
  '/profile',
  '/notifications',
  '/admin/agents',
  '/circle/1/members',
  '/circle/1/edit',
  '/circle/1/agents',
  '/circle/1/moderation',
  '/circle/1/invites',
  '/invites',
  '/circle/managed',
  '/follow/mutual',
  '/messages',
  '/messages/u1'
]

const PUBLIC = [
  '/',
  '/success',
  '/home',
  '/hot',
  '/discover',
  '/user/1',
  '/terms',
  '/privacy',
  '/search',
  '/circle/1',
  '/invite/abc',
  '/post/1',
  '/user/1/following',
  '/user/1/followers'
]

describe('路由表', () => {
  it('需登录的路由集合与预期一致（防止误删 requiresAuth）', () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    const actual = routes.filter(r => r.meta?.requiresAuth).map(r => r.path).sort()
    const expected = PROTECTED.map(p => router.resolve(p).matched[0].path).sort()
    expect(actual).toEqual(expected)
  })

  it('/circle/:id/manage 重定向到内容审核页并保留 query', () => {
    const router = createRouter({ history: createMemoryHistory(), routes })
    const to = router.resolve('/circle/1/manage?tab=reports&focus=p1')
    expect(to.matched[0].redirect).toBeTypeOf('function')
    const target = router.resolve(to.matched[0].redirect(to))
    expect(target.fullPath).toBe('/circle/1/moderation?tab=reports&focus=p1')
  })
})

describe('v0.9.0 关注 / 私信路由', () => {
  const router = createRouter({ history: createMemoryHistory(), routes })

  it.each([
    ['/user/u1/following', 'user-following', { id: 'u1' }],
    ['/user/u1/followers', 'user-followers', { id: 'u1' }],
    ['/follow/mutual', 'follow-mutual', {}],
    ['/messages', 'messages', {}],
    ['/messages/u2', 'message-chat', { userId: 'u2' }]
  ])('%s → %s', (path, name, params) => {
    const to = router.resolve(path)
    expect(to.name).toBe(name)
    expect(to.params).toEqual(params)
    expect(to.meta.titleKey).toMatch(/^title\./)
  })

  it('按路由名生成地址（B / C 用 name + params 跳转）', () => {
    expect(router.resolve({ name: 'user-following', params: { id: 'u1' } }).fullPath).toBe('/user/u1/following')
    expect(router.resolve({ name: 'message-chat', params: { userId: 'u2' } }).fullPath).toBe('/messages/u2')
  })

  it('/user/:id 仍指向用户主页（不被列表路由吞掉）', () => {
    expect(router.resolve('/user/u1').name).toBe('user-detail')
  })
})

describe('authGuard', () => {
  it.each(PROTECTED)('未登录访问 %s 重定向到登录页', async (path) => {
    const router = await setup()
    await router.push(path)
    expect(router.currentRoute.value.name).toBe('login')
    expect(router.currentRoute.value.fullPath).toBe('/')
  })

  it.each(PROTECTED)('已登录访问 %s 放行', async (path) => {
    login()
    const router = await setup()
    await router.push(path)
    expect(router.currentRoute.value.fullPath).toBe(path)
  })

  it('token 已过期视为未登录', async () => {
    login(-1)
    const router = await setup()
    await router.push('/profile')
    expect(router.currentRoute.value.name).toBe('login')
  })

  it('无过期时间的 token 视为有效', async () => {
    localStorage.setItem('qubar_token', 'tok')
    const router = await setup()
    await router.push('/notifications')
    expect(router.currentRoute.value.name).toBe('notifications')
  })

  it.each(PUBLIC)('访客可直接访问 %s', async (path) => {
    const router = await setup('/terms')
    await router.push(path)
    expect(router.currentRoute.value.fullPath).toBe(path)
  })

  it.each(['/nope', '/post', '/a/b/c'])('未匹配路径 %s 落到 404 页', async (path) => {
    const router = await setup()
    await router.push(path)
    expect(router.currentRoute.value.name).toBe('not-found')
  })

  it('直接调用：requiresAuth 且未登录时 next("/")，否则 next()', () => {
    const next = vi.fn()
    authGuard({ meta: { requiresAuth: true } }, null, next)
    expect(next).toHaveBeenLastCalledWith('/')
    login()
    authGuard({ meta: { requiresAuth: true } }, null, next)
    expect(next).toHaveBeenLastCalledWith()
    authGuard({ meta: {} }, null, next)
    expect(next).toHaveBeenCalledTimes(3)
    expect(next).toHaveBeenLastCalledWith()
  })
})

describe('默认导出的 router', () => {
  it('已注册 authGuard 与页面标题同步', async () => {
    const { default: router } = await import('@/router')
    expect(router.getRoutes()).toHaveLength(routes.length)
    // 全局 beforeEach 先于懒加载组件解析执行：未登录时直接重定向，不会加载 UserProfile.vue
    await router.push('/profile')
    expect(router.currentRoute.value.name).toBe('login')
    // afterEach(applyPageTitle)：登录页未配置 titleKey，展示「品牌 - 标语」
    expect(document.title).toMatch(/^Qubar - /)
  })
})
