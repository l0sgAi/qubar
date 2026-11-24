import { describe, it, expect, beforeEach } from 'vitest'
import { applyPageTitle, setDynamicTitleData } from '@/utils/pageTitle'

const postRoute = (query = {}, id = 'p1') => ({
  name: 'post-detail',
  path: `/post/${id}`,
  fullPath: `/post/${id}` + (Object.keys(query).length ? '?' + new URLSearchParams(query) : ''),
  query,
  meta: { titleKey: 'title.postDetail' }
})

describe('pageTitle 动态标题', () => {
  beforeEach(() => {
    applyPageTitle({ meta: {} })
  })

  it('同页内 query 变化（如回复抽屉 ?replies=）保留帖子标题', () => {
    setDynamicTitleData('post-detail', '/post/p1', 'title.postDetailName', { name: '我的帖子' })
    applyPageTitle(postRoute())
    expect(document.title).toContain('我的帖子')

    applyPageTitle(postRoute({ replies: 'c1' }))
    expect(document.title).toContain('我的帖子')
  })

  it('path 变化（换帖）回退通用标题', () => {
    setDynamicTitleData('post-detail', '/post/p1', 'title.postDetailName', { name: '我的帖子' })
    applyPageTitle(postRoute({}, 'p2'))
    expect(document.title).not.toContain('我的帖子')
  })
})
