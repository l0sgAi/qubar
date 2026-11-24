import { describe, it, expect, vi } from 'vitest'
import { homePath, goBack, loginLocation, activeTab } from '@/utils/mobileNav'

describe('mobileNav', () => {
  it('homePath：登录回主页，访客回发现页', () => {
    expect(homePath(true)).toBe('/home')
    expect(homePath(false)).toBe('/discover')
  })

  describe('goBack', () => {
    const makeRouter = () => ({ back: vi.fn(), replace: vi.fn() })

    it('站内有上一页：后退', () => {
      const router = makeRouter()
      goBack(router, true, { back: '/home' })
      expect(router.back).toHaveBeenCalledTimes(1)
      expect(router.replace).not.toHaveBeenCalled()
    })

    it('深链直开（无站内历史）：回落地页而不是退出站点', () => {
      const router = makeRouter()
      goBack(router, true, { back: null })
      expect(router.replace).toHaveBeenCalledWith('/home')
      goBack(router, false, null)
      expect(router.replace).toHaveBeenCalledWith('/discover')
      expect(router.back).not.toHaveBeenCalled()
    })

    it('默认读取 window.history.state', () => {
      const router = makeRouter()
      vi.spyOn(window.history, 'state', 'get').mockReturnValue({ back: '/hot' })
      goBack(router, false)
      expect(router.back).toHaveBeenCalledTimes(1)
    })
  })

  it('loginLocation：带 redirect 回到当前页；登录页自身回退到主页', () => {
    expect(loginLocation('/post/1?comment_id=2')).toEqual({ path: '/', query: { redirect: '/post/1?comment_id=2', tab: 'login' } })
    expect(loginLocation('/')).toEqual({ path: '/', query: { redirect: '/home', tab: 'login' } })
    expect(loginLocation('')).toEqual({ path: '/', query: { redirect: '/home', tab: 'login' } })
  })

  it.each([
    ['/home', 'home'],
    ['/discover', 'discover'],
    ['/hot', 'discover'],
    ['/search', 'discover'],
    ['/notifications', 'notice'],
    ['/messages', 'messages'],
    ['/messages/u1', 'messages'],
    ['/messagesx', null],
    ['/profile', 'me'],
    ['/invites', 'me'],
    ['/invite/abc', null],
    ['/post/1', null],
    ['/circle/1', null]
  ])('activeTab(%s) → %s', (path, tab) => {
    expect(activeTab(path)).toBe(tab)
  })
})
