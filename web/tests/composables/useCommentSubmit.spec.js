import { describe, it, expect, vi, beforeEach } from 'vitest'

const api = vi.hoisted(() => ({
  createComment: vi.fn(),
  getUserInfo: vi.fn(),
  seedContentMentions: vi.fn()
}))
vi.mock('@/api/comment', async (importOriginal) => ({
  ...(await importOriginal()),
  createComment: api.createComment
}))
vi.mock('@/api/auth', () => ({ getUserInfo: api.getUserInfo }))
vi.mock('@/utils/mentionResolve', () => ({ seedContentMentions: api.seedContentMentions }))

const { useCommentSubmit, commentSubmitErrorMessage } = await import('@/composables/useCommentSubmit')

const ME = { id: 'u1', name: '我', avatar_url: 'a.png' }

describe('useCommentSubmit', () => {
  beforeEach(() => {
    api.createComment.mockReset()
    api.getUserInfo.mockReset().mockResolvedValue({ data: ME })
    api.seedContentMentions.mockReset()
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-10-02T00:00:00Z'))
  })

  it('顶层评论：请求体与原 CommentEditor 一致（无 @ 时不传 mention_user_ids）', async () => {
    api.createComment.mockResolvedValue({ data: { id: 'c1' } })
    const { submit } = useCommentSubmit()
    await submit({ postId: 'p1', content: 'hi' })
    expect(api.createComment).toHaveBeenCalledWith({
      post_id: 'p1',
      content: 'hi',
      extra_data: null,
      mention_user_ids: undefined
    })
  })

  it('顶层评论：带图片与 @', async () => {
    api.createComment.mockResolvedValue({ data: { id: 'c1' } })
    const { submit } = useCommentSubmit()
    const created = await submit({ postId: 'p1', content: 'hi', extraData: { images: ['x'] }, mentionIds: ['u2'] })
    expect(api.createComment.mock.calls[0][0]).toMatchObject({ extra_data: { images: ['x'] }, mention_user_ids: ['u2'] })
    expect(created.extra_data).toEqual({ images: ['x'] })
  })

  it('顶层评论：乐观渲染对象补全作者与计数，并回灌 mentions', async () => {
    api.createComment.mockResolvedValue({ data: { id: 'c1' } })
    const { submit } = useCommentSubmit()
    const created = await submit({ postId: 'p1', content: 'hi' })
    expect(created).toEqual({
      id: 'c1',
      author_name: '我',
      author_id: 'u1',
      author_avatar: 'a.png',
      content: 'hi',
      like_count: 0,
      reply_count: 0,
      create_time: '2026-10-02T00:00:00.000Z'
    })
    expect(api.seedContentMentions).toHaveBeenCalledWith([created])
  })

  it('后端只回 id（非对象）时也能组装', async () => {
    api.createComment.mockResolvedValue({ data: 'c9' })
    const { submit } = useCommentSubmit()
    const created = await submit({ postId: 'p1', content: 'hi' })
    expect(created.id).toBe('c9')
  })

  it('回复根评论：root_id 取 rootId，reply_to_id 为 null', async () => {
    api.createComment.mockResolvedValue({ data: { id: 'r1' } })
    const { submit } = useCommentSubmit()
    const created = await submit({ postId: 'p1', content: 'yo', reply: { rootId: 'c1', replyToId: null, replyToName: 'Bob' } })
    expect(api.createComment).toHaveBeenCalledWith({
      post_id: 'p1',
      root_id: 'c1',
      reply_to_id: null,
      content: 'yo',
      extra_data: null,
      mention_user_ids: undefined
    })
    expect(created.reply_to_name).toBe('Bob')
    expect(created).not.toHaveProperty('reply_count')
  })

  it('回复某条回复：reply_to_id 为该回复', async () => {
    api.createComment.mockResolvedValue({ data: { id: 'r2', reply_to_name: '服务端名' } })
    const { submit } = useCommentSubmit()
    const created = await submit({ postId: 'p1', content: 'yo', reply: { rootId: 'c1', replyToId: 'r1', replyToName: 'Bob' } })
    expect(api.createComment.mock.calls[0][0]).toMatchObject({ root_id: 'c1', reply_to_id: 'r1' })
    // 服务端回传的 reply_to_name 优先
    expect(created.reply_to_name).toBe('服务端名')
  })

  it('获取用户信息失败时仍返回对象（作者信息取后端回传）', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    api.getUserInfo.mockRejectedValue(new Error('net'))
    api.createComment.mockResolvedValue({ data: { id: 'c1', author_name: '后端名' } })
    const { submit } = useCommentSubmit()
    const created = await submit({ postId: 'p1', content: 'hi' })
    expect(created.author_name).toBe('后端名')
  })

  it('submitting：请求中为 true，结束后复位', async () => {
    let resolve
    api.createComment.mockReturnValue(new Promise(r => { resolve = r }))
    const { submit, submitting } = useCommentSubmit()
    const p = submit({ postId: 'p1', content: 'hi' })
    expect(submitting.value).toBe(true)
    resolve({ data: { id: 'c1' } })
    await p
    expect(submitting.value).toBe(false)
  })

  it('接口失败：抛出原始错误，submitting 复位', async () => {
    const err = new Error('boom')
    api.createComment.mockRejectedValue(err)
    const { submit, submitting } = useCommentSubmit()
    await expect(submit({ postId: 'p1', content: 'hi' })).rejects.toBe(err)
    expect(submitting.value).toBe(false)
  })
})

describe('commentSubmitErrorMessage（后端 #170：帖子不可见时评论返回 404）', () => {
  const t = (key) => `t:${key}`
  const err = (fields) => Object.assign(new Error('backend english'), fields)

  it('帖子不可见（40401 / 信封 204 / HTTP 404）：自有文案，不展示后端 message', () => {
    expect(commentSubmitErrorMessage(err({ code: 40401 }), t, 'comment.editor.failed')).toBe('t:comment.unavailable')
    expect(commentSubmitErrorMessage(err({ code: 204 }), t, 'comment.editor.failed')).toBe('t:comment.unavailable')
    expect(commentSubmitErrorMessage(err({ status: 404 }), t, 'comment.editor.failed')).toBe('t:comment.unavailable')
  })

  it('其它错误：沿用后端 message，缺省时用 fallbackKey', () => {
    expect(commentSubmitErrorMessage(err({ code: 210 }), t, 'comment.editor.failed')).toBe('backend english')
    expect(commentSubmitErrorMessage({}, t, 'comment.reply.failed')).toBe('t:comment.reply.failed')
  })
})
