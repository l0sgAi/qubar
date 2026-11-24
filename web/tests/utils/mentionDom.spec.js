import { describe, expect, it } from 'vitest'
import { applyMentionLinks } from '@/utils/mentionDom'

describe('渲染内容自带的提及清单', () => {
  it('只链接正文中的已知用户，保留代码块、已有链接和未命中的文本', () => {
    const root = document.createElement('article')
    root.innerHTML = '<p>@Alice 与 @Nobody</p><code>@Alice</code><a href="/help">@Alice</a>'
    const mentions = [{ id: 'user-1', username: 'Alice' }]

    expect(applyMentionLinks(root, mentions)).toBe(true)
    const link = root.querySelector('p a')
    expect(link.getAttribute('href')).toBe('/user/user-1')
    expect(link.textContent).toBe('@Alice')
    expect(root.querySelector('p').textContent).toBe('@Alice 与 @Nobody')
    expect(root.querySelector('code').innerHTML).toBe('@Alice')
    expect(root.querySelector('a[href="/help"]').innerHTML).toBe('@Alice')
    expect(applyMentionLinks(root, mentions)).toBe(false)
  })
})
