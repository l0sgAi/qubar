import { describe, it, expect, vi } from 'vitest'
import { getMobileMeta, isUnsupportedOnMobile, MOBILE_META_DEFAULTS, MOBILE_TOP_BAR } from '@/router/mobileMeta'

// 登录页被路由表静态导入，这里替换为空组件，避免加载整页依赖
vi.mock('@/views/auth/Login.vue', () => ({ default: { render: () => null } }))

const { routes } = await import('@/router')

const ALLOWED_KEYS = Object.keys(MOBILE_META_DEFAULTS)

describe('getMobileMeta', () => {
  it('无 meta.mobile 时取默认值', () => {
    expect(getMobileMeta({ meta: {} })).toEqual({ supported: true, topBar: 'main', tabBar: true })
    expect(getMobileMeta(undefined)).toEqual(MOBILE_META_DEFAULTS)
  })

  it('部分覆盖时与默认值合并', () => {
    expect(getMobileMeta({ meta: { mobile: { topBar: 'back' } } }))
      .toEqual({ supported: true, topBar: 'back', tabBar: true })
  })

  it('默认值不可被意外改写', () => {
    expect(Object.isFrozen(MOBILE_META_DEFAULTS)).toBe(true)
  })
})

describe('路由表 meta.mobile', () => {
  const withMobile = routes.filter(r => r.meta?.mobile)

  it.each(withMobile.map(r => [r.path, r.meta.mobile]))('%s 取值合法', (_, mobile) => {
    Object.keys(mobile).forEach(key => expect(ALLOWED_KEYS).toContain(key))
    if ('topBar' in mobile) expect(MOBILE_TOP_BAR).toContain(mobile.topBar)
    if ('tabBar' in mobile) expect(typeof mobile.tabBar).toBe('boolean')
    if ('supported' in mobile) expect(typeof mobile.supported).toBe('boolean')
  })

  it('创作 / 管理类页面在移动端不支持', () => {
    const unsupported = routes
      .filter(r => getMobileMeta(r).supported === false)
      .map(r => r.path)
      .sort()
    expect(unsupported).toEqual([
      '/admin/agents',
      '/circle/:id/agents',
      '/circle/:id/edit',
      '/circle/:id/members',
      '/circle/:id/moderation',
      '/circle/managed',
      '/create-post'
    ])
  })

  it('私信聊天页：返回式顶栏、无 TabBar；关注列表：返回式顶栏', () => {
    expect(getMobileMeta(routes.find(r => r.name === 'message-chat')))
      .toEqual({ supported: true, topBar: 'back', tabBar: false })
    expect(getMobileMeta(routes.find(r => r.name === 'user-following')).topBar).toBe('back')
    expect(getMobileMeta(routes.find(r => r.name === 'messages'))).toEqual(MOBILE_META_DEFAULTS)
  })

  it('帖子详情：返回式顶栏、无 TabBar', () => {
    const route = routes.find(r => r.name === 'post-detail')
    expect(getMobileMeta(route)).toEqual({ supported: true, topBar: 'back', tabBar: false })
  })
})

describe('isUnsupportedOnMobile', () => {
  const unsupported = { meta: { mobile: { supported: false } } }
  const supported = { meta: {} }

  it.each([
    [unsupported, true, true],
    [unsupported, false, false],
    [supported, true, false],
    [supported, false, false]
  ])('route=%o mobileShell=%s → %s', (route, mobileShell, expected) => {
    expect(isUnsupportedOnMobile(route, mobileShell)).toBe(expected)
  })
})
