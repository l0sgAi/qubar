import { describe, it, expect, vi, afterEach } from 'vitest'
import { copyText, shareOrCopy, postSharePath, postShareUrl, isShareRetryableError, shareErrorKey } from '@/utils/share'

// navigator.clipboard / share 在 jsdom 中不存在或只读，用 defineProperty 注入并在用例后移除
const setNavigator = (key, value) => {
  Object.defineProperty(navigator, key, { value, configurable: true, writable: true })
}

afterEach(() => {
  delete navigator.clipboard
  delete navigator.share
  delete document.execCommand
})

describe('copyText', () => {
  it('优先 Clipboard API', async () => {
    const writeText = vi.fn().mockResolvedValue()
    setNavigator('clipboard', { writeText })
    expect(await copyText('x')).toBe(true)
    expect(writeText).toHaveBeenCalledWith('x')
  })

  it('Clipboard API 被拒时回落 execCommand', async () => {
    setNavigator('clipboard', { writeText: vi.fn().mockRejectedValue(new Error('denied')) })
    document.execCommand = vi.fn().mockReturnValue(true)
    expect(await copyText('x')).toBe(true)
    expect(document.execCommand).toHaveBeenCalledWith('copy')
    // 临时 textarea 已移除
    expect(document.querySelector('textarea')).toBeNull()
  })

  it('都不可用时返回 false', async () => {
    document.execCommand = vi.fn(() => { throw new Error('unsupported') })
    expect(await copyText('x')).toBe(false)
    // 复制抛错时临时 textarea 也要移除
    expect(document.querySelector('textarea')).toBeNull()
  })
})

describe('shareOrCopy', () => {
  it('支持系统分享：调起分享面板', async () => {
    const share = vi.fn().mockResolvedValue()
    setNavigator('share', share)
    expect(await shareOrCopy({ title: 't', url: 'u' })).toBe('shared')
    expect(share).toHaveBeenCalledWith({ title: 't', url: 'u' })
  })

  it('用户关闭分享面板：不再复制', async () => {
    setNavigator('share', vi.fn().mockRejectedValue(Object.assign(new Error('abort'), { name: 'AbortError' })))
    const writeText = vi.fn().mockResolvedValue()
    setNavigator('clipboard', { writeText })
    expect(await shareOrCopy({ title: 't', url: 'u' })).toBe('cancelled')
    expect(writeText).not.toHaveBeenCalled()
  })

  it('分享失败（非取消）或不支持：复制链接', async () => {
    setNavigator('share', vi.fn().mockRejectedValue(new Error('NotAllowed')))
    setNavigator('clipboard', { writeText: vi.fn().mockResolvedValue() })
    expect(await shareOrCopy({ title: 't', url: 'u' })).toBe('copied')
    delete navigator.share
    expect(await shareOrCopy({ title: 't', url: 'u' })).toBe('copied')
  })

  it('复制也失败：返回 failed', async () => {
    document.execCommand = vi.fn().mockReturnValue(false)
    expect(await shareOrCopy({ title: 't', url: 'u' })).toBe('failed')
  })
})

describe('帖子分享链接与错误（后端 v0.9.0 POST /post/share）', () => {
  const apiErr = (code, status) => Object.assign(new Error('x'), { code, status })

  it('链接只含帖子 ID，默认用当前 origin', () => {
    expect(postSharePath('p1')).toBe('/post/p1')
    expect(postSharePath('a/b')).toBe('/post/a%2Fb')
    expect(postShareUrl('p1', 'https://qubar.site')).toBe('https://qubar.site/post/p1')
    expect(postShareUrl('p1')).toBe(`${window.location.origin}/post/p1`)
  })

  it('503 / 212 可重试', () => {
    expect(isShareRetryableError(apiErr(212, 503))).toBe(true)
    expect(isShareRetryableError(apiErr(210, 500))).toBe(false)
  })

  it.each([
    [apiErr(204, 404), 'share.error.notFound'],
    [apiErr(212, 503), 'share.error.retry'],
    [apiErr(202, 401), null],
    [apiErr(210, 500), null],
    [new Error('Network Error'), null]
  ])('shareErrorKey(%o) → %s', (err, key) => {
    expect(shareErrorKey(err)).toBe(key)
  })
})
