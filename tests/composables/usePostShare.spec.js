import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import { ref } from 'vue'
import { auth } from '@/utils/auth'

const mocks = vi.hoisted(() => ({ sharePost: vi.fn() }))
vi.mock('@/api/post', () => ({ sharePost: mocks.sharePost }))

const {
  resolvePostShare,
  usePostShareState,
  applyShareResult,
  recordPostShare,
  sharePostLink,
  __resetPostShare
} = await import('@/composables/usePostShare')

const login = (token = 'tok-a') => auth.setToken(token, 60)

const deferred = () => {
  let resolve
  const promise = new Promise(r => { resolve = r })
  return { promise, resolve }
}

const apiError = (status, code) => Object.assign(new Error('x'), { status, code })

describe('usePostShare', () => {
  beforeEach(() => {
    __resetPostShare()
    mocks.sharePost.mockReset()
  })

  describe('resolvePostShare / applyShareResult', () => {
    it('没有覆盖值时原样返回接口字段；缺字段按「无分享数、未分享」', () => {
      expect(resolvePostShare('p1', { count: 3, shared: true })).toEqual({ count: 3, shared: true })
      expect(resolvePostShare('p1')).toEqual({ count: null, shared: false })
      expect(resolvePostShare('p1', { count: undefined, shared: undefined })).toEqual({ count: null, shared: false })
      expect(resolvePostShare('p1', { count: null, shared: null })).toEqual({ count: null, shared: false })
      expect(resolvePostShare('p1', { count: '' })).toEqual({ count: null, shared: false })
      expect(resolvePostShare('p1', { count: 0 })).toEqual({ count: 0, shared: false })
      expect(resolvePostShare('p1', { count: -1 })).toEqual({ count: null, shared: false })
      expect(resolvePostShare('p1', { count: 'abc' })).toEqual({ count: null, shared: false })
    })

    it('分享成功后覆盖分享数并置已分享；只增不减', () => {
      login()
      applyShareResult('p1', { share_count: 5, first_share: true })
      expect(resolvePostShare('p1', { count: 4 })).toEqual({ count: 5, shared: true })
      // 列表接口没给分享数时也用覆盖值
      expect(resolvePostShare('p1')).toEqual({ count: 5, shared: true })
      // 之后拉到更大的值以接口为准
      expect(resolvePostShare('p1', { count: 9 }).count).toBe(9)
      // 后端短暂偏低的返回不会让覆盖值倒退
      applyShareResult('p1', { share_count: 2 })
      expect(resolvePostShare('p1').count).toBe(5)
    })

    it('响应缺 share_count 时只置已分享', () => {
      login()
      applyShareResult('p1', {})
      expect(resolvePostShare('p1', { count: 1 })).toEqual({ count: 1, shared: true })
      applyShareResult('p2', null)
      expect(resolvePostShare('p2')).toEqual({ count: null, shared: true })
    })

    it('是否已分享按账号隔离，分享数全局共享', () => {
      login('tok-a')
      applyShareResult('p1', { share_count: 1 })
      login('tok-b')
      expect(resolvePostShare('p1')).toEqual({ count: 1, shared: false })
      login('tok-a')
      expect(resolvePostShare('p1').shared).toBe(true)
    })

    it('usePostShareState 随参数与覆盖值响应式更新', () => {
      login()
      const id = ref('p1')
      const count = ref(1)
      const state = usePostShareState(id, count, () => false)
      expect(state.value).toEqual({ count: 1, shared: false })
      count.value = 2
      expect(state.value.count).toBe(2)
      applyShareResult('p1', { share_count: 3 })
      expect(state.value).toEqual({ count: 3, shared: true })
      id.value = 'p2'
      expect(state.value).toEqual({ count: 2, shared: false })
    })
  })

  describe('recordPostShare', () => {
    it('成功：返回计数结果并写入覆盖层', async () => {
      login()
      mocks.sharePost.mockResolvedValue({ data: { share_count: 8, first_share: true } })
      await expect(recordPostShare('p1')).resolves.toEqual({ share_count: 8, first_share: true })
      expect(mocks.sharePost).toHaveBeenCalledWith('p1')
      expect(resolvePostShare('p1')).toEqual({ count: 8, shared: true })
    })

    it('503 自动重试一次', async () => {
      login()
      mocks.sharePost
        .mockRejectedValueOnce(apiError(503, 212))
        .mockResolvedValueOnce({ data: { share_count: 1, first_share: true } })
      await expect(recordPostShare('p1', { retryDelay: 0 })).resolves.toEqual({ share_count: 1, first_share: true })
      expect(mocks.sharePost).toHaveBeenCalledTimes(2)
    })

    it('重试后仍 503 → 抛出，不写覆盖层', async () => {
      login()
      mocks.sharePost.mockRejectedValue(apiError(503, 212))
      await expect(recordPostShare('p1', { retryDelay: 0 })).rejects.toMatchObject({ status: 503 })
      expect(mocks.sharePost).toHaveBeenCalledTimes(2)
      expect(resolvePostShare('p1')).toEqual({ count: null, shared: false })
    })

    it('404 不重试', async () => {
      login()
      mocks.sharePost.mockRejectedValue(apiError(404, 204))
      await expect(recordPostShare('p1', { retryDelay: 0 })).rejects.toMatchObject({ status: 404 })
      expect(mocks.sharePost).toHaveBeenCalledTimes(1)
    })

    it('并发点击共用在途请求，first_share 只给第一次', async () => {
      login()
      let resolve
      mocks.sharePost.mockReturnValue(new Promise(r => { resolve = r }))
      const a = recordPostShare('p1')
      const b = recordPostShare('p1')
      resolve({ data: { share_count: 1, first_share: true } })
      await expect(a).resolves.toEqual({ share_count: 1, first_share: true })
      await expect(b).resolves.toEqual({ share_count: 1, first_share: false })
      expect(mocks.sharePost).toHaveBeenCalledTimes(1)
      // 结束后再点会重新请求
      mocks.sharePost.mockResolvedValue({ data: { share_count: 1, first_share: false } })
      await recordPostShare('p1')
      expect(mocks.sharePost).toHaveBeenCalledTimes(2)
    })

    it('在途请求失败后清掉，下次重新请求', async () => {
      login()
      mocks.sharePost.mockRejectedValueOnce(apiError(500, 210))
      await expect(recordPostShare('p1')).rejects.toBeTruthy()
      mocks.sharePost.mockResolvedValueOnce({ data: { share_count: 2, first_share: true } })
      await expect(recordPostShare('p1')).resolves.toMatchObject({ share_count: 2 })
    })

    it('响应没有 data 时按未首次分享处理', async () => {
      login()
      mocks.sharePost.mockResolvedValue({})
      await expect(recordPostShare('p1')).resolves.toEqual({ share_count: undefined, first_share: false })
    })
  })

  describe('sharePostLink', () => {
    const stubShare = (impl) => vi.stubGlobal('navigator', { ...navigator, share: impl, clipboard: undefined })
    const stubClipboard = (writeText) => vi.stubGlobal('navigator', { ...navigator, share: undefined, clipboard: { writeText } })

    it.each(['tok-b', null])('A 的慢响应更新公共计数，只给发起者高亮（当前会话 %s）', async (nextToken) => {
      login()
      stubClipboard(vi.fn().mockResolvedValue())
      const response = deferred()
      mocks.sharePost.mockReturnValue(response.promise)
      const state = usePostShareState('p1')
      const request = sharePostLink({ postId: 'p1' })
      await flushPromises()
      expect(mocks.sharePost).toHaveBeenCalledTimes(1)
      if (nextToken) login(nextToken)
      else auth.clearToken()
      response.resolve({ data: { share_count: 7, first_share: true } })
      expect(await request).toMatchObject({ counted: true, firstShare: false })
      expect(state.value).toEqual({ count: 7, shared: false })
      login()
      expect(state.value).toEqual({ count: 7, shared: true })
    })

    it.each(['tok-b', null])('503 延时重试中切到 %s，停止写入并静默结束', async (nextToken) => {
      vi.useFakeTimers()
      login()
      stubClipboard(vi.fn().mockResolvedValue())
      mocks.sharePost.mockRejectedValue(apiError(503, 212))
      const request = sharePostLink({ postId: 'p1' })
      await vi.advanceTimersByTimeAsync(300)
      expect(mocks.sharePost).toHaveBeenCalledTimes(1)
      if (nextToken) login(nextToken)
      else auth.clearToken()
      await vi.advanceTimersByTimeAsync(1000)
      expect(await request).toMatchObject({ counted: false, firstShare: false, errorKey: null })
      expect(mocks.sharePost).toHaveBeenCalledTimes(1)
      expect(resolvePostShare('p1')).toEqual({ count: null, shared: false })
    })

    it('系统面板打开期间切账号，完成分享后跳过计数', async () => {
      login()
      const panel = deferred()
      stubShare(() => panel.promise)
      const request = sharePostLink({ postId: 'p1' })
      login('tok-b')
      panel.resolve()
      expect(await request).toMatchObject({ result: 'shared', counted: false })
      expect(mocks.sharePost).not.toHaveBeenCalled()
    })

    it('系统分享成功 + 登录 → 计数，链接只有帖子路径', async () => {
      login()
      const share = vi.fn().mockResolvedValue()
      stubShare(share)
      mocks.sharePost.mockResolvedValue({ data: { share_count: 3, first_share: true } })
      const outcome = await sharePostLink({ postId: 'p1', title: 'T' })
      expect(share).toHaveBeenCalledWith({ title: 'T', url: `${window.location.origin}/post/p1` })
      expect(outcome).toEqual({ result: 'shared', counted: true, firstShare: true, errorKey: null })
    })

    it('用户取消系统分享 → 不计数', async () => {
      login()
      stubShare(vi.fn().mockRejectedValue(Object.assign(new Error('abort'), { name: 'AbortError' })))
      const outcome = await sharePostLink({ postId: 'p1' })
      expect(outcome.result).toBe('cancelled')
      expect(outcome.counted).toBe(false)
      expect(mocks.sharePost).not.toHaveBeenCalled()
    })

    it('不支持系统分享时复制链接；访客不调接口', async () => {
      const writeText = vi.fn().mockResolvedValue()
      stubClipboard(writeText)
      const outcome = await sharePostLink({ postId: 'a b' })
      expect(writeText).toHaveBeenCalledWith(`${window.location.origin}/post/a%20b`)
      expect(outcome).toEqual({ result: 'copied', counted: false, firstShare: false, errorKey: null })
      expect(mocks.sharePost).not.toHaveBeenCalled()
    })

    it('复制失败 → 不计数', async () => {
      login()
      stubClipboard(vi.fn().mockRejectedValue(new Error('denied')))
      document.execCommand = vi.fn().mockReturnValue(false)
      const outcome = await sharePostLink({ postId: 'p1' })
      expect(outcome.result).toBe('failed')
      expect(mocks.sharePost).not.toHaveBeenCalled()
    })

    it('计数失败不影响分享：404 → notFound，其它静默', async () => {
      login()
      stubClipboard(vi.fn().mockResolvedValue())
      mocks.sharePost.mockRejectedValueOnce(apiError(404, 204))
      expect(await sharePostLink({ postId: 'p1' })).toEqual({
        result: 'copied', counted: false, firstShare: false, errorKey: 'share.error.notFound'
      })
      mocks.sharePost.mockRejectedValueOnce(apiError(500, 210))
      expect((await sharePostLink({ postId: 'p1' })).errorKey).toBe(null)
    })

    it('重试后仍 503 → 提示可重试', async () => {
      login()
      stubClipboard(vi.fn().mockResolvedValue())
      mocks.sharePost.mockRejectedValue(apiError(503, 212))
      const outcome = await sharePostLink({ postId: 'p1' }, { retryDelay: 0 })
      expect(outcome.errorKey).toBe('share.error.retry')
      expect(mocks.sharePost).toHaveBeenCalledTimes(2)
    })
  })
})
