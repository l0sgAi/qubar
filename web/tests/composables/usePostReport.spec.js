import { describe, it, expect, vi, beforeEach } from 'vitest'

const api = vi.hoisted(() => ({ getUserInfo: vi.fn() }))
vi.mock('@/api/auth', () => ({ getUserInfo: api.getUserInfo }))

const {
  isPostReported,
  markPostReported,
  ensureViewer,
  canReportPost,
  reportErrorKey,
  __resetPostReport
} = await import('@/composables/usePostReport')

const login = (token = 'tok-a') => {
  localStorage.setItem('qubar_token', token)
  localStorage.setItem('qubar_token_expire', String(Date.now() + 60_000))
}

describe('usePostReport', () => {
  beforeEach(() => {
    __resetPostReport()
    api.getUserInfo.mockReset().mockResolvedValue({ data: { id: 'me' } })
  })

  describe('已举报集合', () => {
    it('标记后本次会话内记住', () => {
      login()
      expect(isPostReported('p1')).toBe(false)
      markPostReported('p1')
      expect(isPostReported('p1')).toBe(true)
      expect(isPostReported('p2')).toBe(false)
    })

    it('换账号（token 变化）后不再算已举报', () => {
      login('tok-a')
      markPostReported('p1')
      login('tok-b')
      expect(isPostReported('p1')).toBe(false)
      login('tok-a')
      expect(isPostReported('p1')).toBe(true)
    })
  })

  describe('当前用户 / 入口可见性', () => {
    it('访客：已发布的帖子显示入口，不请求用户信息', async () => {
      expect(await ensureViewer()).toBe(null)
      expect(api.getUserInfo).not.toHaveBeenCalled()
      expect(canReportPost({ status: 1, authorId: 'u2' })).toBe(true)
    })

    it('未发布（审核中 / 驳回 / 屏蔽）或状态未知：不显示', () => {
      for (const status of [0, 2, 3, 4, null, undefined]) {
        expect(canReportPost({ status, authorId: 'u2' })).toBe(false)
      }
    })

    it('已登录：拿到当前用户前不显示；之后自己的帖子不显示、别人的显示', async () => {
      login()
      expect(canReportPost({ status: 1, authorId: 'u2' })).toBe(false)
      expect(await ensureViewer()).toBe('me')
      expect(canReportPost({ status: 1, authorId: 'me' })).toBe(false)
      expect(canReportPost({ status: 1, authorId: 'u2' })).toBe(true)
      // 作者未知：显示，由后端兜底
      expect(canReportPost({ status: 1, authorId: null })).toBe(true)
    })

    it('同一 token 只请求一次，并发调用共用请求', async () => {
      login()
      const [a, b] = await Promise.all([ensureViewer(), ensureViewer()])
      expect(a).toBe('me')
      expect(b).toBe('me')
      expect(await ensureViewer()).toBe('me')
      expect(api.getUserInfo).toHaveBeenCalledTimes(1)
    })

    it('换 token 后重新请求', async () => {
      login('tok-a')
      await ensureViewer()
      login('tok-b')
      api.getUserInfo.mockResolvedValue({ data: { id: 'other' } })
      expect(canReportPost({ status: 1, authorId: 'me' })).toBe(false)
      expect(await ensureViewer()).toBe('other')
      expect(canReportPost({ status: 1, authorId: 'me' })).toBe(true)
      expect(api.getUserInfo).toHaveBeenCalledTimes(2)
    })

    it('用户信息请求失败：照常显示入口（自己的帖子由后端 400 兜底）', async () => {
      login()
      api.getUserInfo.mockRejectedValue(new Error('net'))
      expect(await ensureViewer()).toBe(null)
      expect(canReportPost({ status: 1, authorId: 'me' })).toBe(true)
    })

    it('响应缺少 id：同样按未知处理', async () => {
      login()
      api.getUserInfo.mockResolvedValue({ data: null })
      expect(await ensureViewer()).toBe(null)
      expect(canReportPost({ status: 1, authorId: 'me' })).toBe(true)
    })
  })

  describe('reportErrorKey', () => {
    it.each([
      [{ code: 207, status: 409 }, 'already'],
      [{ status: 409 }, 'already'],
      [{ code: 207 }, 'already'],
      [{ code: 208, status: 429 }, 'tooOften'],
      [{ status: 429 }, 'tooOften'],
      [{ code: 208 }, 'tooOften'],
      [{ code: 204, status: 404 }, 'unavailable'],
      [{ status: 404 }, 'unavailable'],
      [{ code: 201, status: 400 }, 'invalid'],
      [{ status: 400 }, 'invalid'],
      [new Error('Network Error'), 'failed'],
      [undefined, 'failed']
    ])('%o → %s', (error, key) => {
      expect(reportErrorKey(error)).toBe(key)
    })
  })
})
