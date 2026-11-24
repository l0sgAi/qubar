import { describe, it, expect } from 'vitest'
import { getUserId, userUrl, getCommentImages, getReplyToName, getReplyToUserId } from '@/utils/commentDisplay'

describe('commentDisplay', () => {
  it('getUserId：兼容多种字段名，缺失时不回退到评论 id', () => {
    expect(getUserId({ author_id: 'a' })).toBe('a')
    expect(getUserId({ user_id: 'b' })).toBe('b')
    expect(getUserId({ userId: 'c' })).toBe('c')
    expect(getUserId({ authorId: 'd' })).toBe('d')
    expect(getUserId({ id: 'comment-1' })).toBeNull()
  })

  it('userUrl：有 id 生成主页链接，无 id 返回 null', () => {
    expect(userUrl('u1')).toBe('/user/u1')
    expect(userUrl(null)).toBeNull()
  })

  it('getCommentImages：支持字符串 / 对象 extra_data，异常与缺失返回空数组', () => {
    expect(getCommentImages({})).toEqual([])
    expect(getCommentImages({ extra_data: '{"images":["a.png"]}' })).toEqual(['a.png'])
    expect(getCommentImages({ extra_data: { images: ['b.png'] } })).toEqual(['b.png'])
    expect(getCommentImages({ extra_data: { other: 1 } })).toEqual([])
    expect(getCommentImages({ extra_data: '{bad json' })).toEqual([])
  })

  it('getReplyToName / getReplyToUserId：优先后端字段，否则从同列表查找', () => {
    const replies = [{ id: 'r1', author_name: 'Alice', author_id: 'u1' }]
    expect(getReplyToName({ reply_to_name: 'Bob' }, replies)).toBe('Bob')
    expect(getReplyToUserId({ reply_to_user_id: 'u2' }, replies)).toBe('u2')
    expect(getReplyToName({ reply_to_id: 'r1' }, replies)).toBe('Alice')
    expect(getReplyToUserId({ reply_to_id: 'r1' }, replies)).toBe('u1')
    expect(getReplyToName({ reply_to_id: 'missing' }, replies)).toBeUndefined()
    expect(getReplyToName({ reply_to_id: 'r1' }, null)).toBeNull()
    expect(getReplyToUserId({}, replies)).toBeNull()
  })
})
