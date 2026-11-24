import { describe, it, expect } from 'vitest'
import { isGuestAccessible, isFeedTabRestricted, GUEST_ACCESSIBLE } from '@/utils/guest-access'

describe('isGuestAccessible', () => {
  it.each(GUEST_ACCESSIBLE.map(r => [r.method, r.path]))('白名单 %s %s 可读', (method, path) => {
    expect(isGuestAccessible(method, path)).toBe(true)
  })

  it('前缀匹配带 id 的子路径', () => {
    expect(isGuestAccessible('GET', '/post/detail/123')).toBe(true)
    expect(isGuestAccessible('GET', '/circle/detail/9/extra')).toBe(true)
  })

  it('前缀必须在路径段边界上', () => {
    expect(isGuestAccessible('GET', '/post/detailx')).toBe(false)
    expect(isGuestAccessible('GET', '/trendingfoo')).toBe(false)
  })

  it('方法大小写不敏感，但方法必须匹配', () => {
    expect(isGuestAccessible('get', '/post/list')).toBe(true)
    expect(isGuestAccessible('POST', '/post/list')).toBe(false)
    expect(isGuestAccessible('DELETE', '/post/detail/1')).toBe(false)
  })

  it('规范化完整 URL / query / hash / 尾斜杠', () => {
    expect(isGuestAccessible('GET', 'https://api.qubar.site/post/detail/1?x=1')).toBe(true)
    expect(isGuestAccessible('GET', '/comment/list?post_id=1#top')).toBe(true)
    expect(isGuestAccessible('GET', '/trending/')).toBe(true)
  })

  it.each([
    ['/circle/random?size=3', '/circle/join'],
    ['/circle/invite/preview?token=example', '/circle/invite/accept'],
    ['/comment/locate?comment_id=c1', '/comment/create']
  ])('访客 GET %s 可读，对应 POST %s 需要登录', (read, write) => {
    expect(isGuestAccessible('GET', read)).toBe(true)
    expect(isGuestAccessible('POST', write)).toBe(false)
  })

  it('非白名单接口不可读', () => {
    expect(isGuestAccessible('GET', '/user/info')).toBe(false)
    expect(isGuestAccessible('GET', '/notice/list')).toBe(false)
  })

  it.each([
    ['/post/home', false],
    ['/post/home?tab=', false],
    ['/post/home?tab=unknown', false],
    ['/post/home?tab=latest#top', true],
    ['/post/home?tab=hot', true],
    ['/post/home?tab=latest&page=2', true],
    ['/post/home?tab=recommend', false],
    ['/post/home?tab=following', false]
  ])('/post/home 按 tab 特判：%s → %s', (url, expected) => {
    expect(isGuestAccessible('GET', url)).toBe(expected)
  })

  it('/post/home 非 GET 不可读', () => {
    expect(isGuestAccessible('POST', '/post/home?tab=hot')).toBe(false)
  })

  it.each([
    [undefined, '/post/list'],
    ['GET', undefined],
    ['', ''],
    [null, null]
  ])('缺少入参（%p, %p）返回 false', (method, url) => {
    expect(isGuestAccessible(method, url)).toBe(false)
  })
})

describe('isFeedTabRestricted', () => {
  it('仅精确匹配后端约定的 message', () => {
    expect(isFeedTabRestricted('This feed tab requires login')).toBe(true)
    expect(isFeedTabRestricted('this feed tab requires login')).toBe(false)
    expect(isFeedTabRestricted('Not login')).toBe(false)
    expect(isFeedTabRestricted(undefined)).toBe(false)
  })
})
