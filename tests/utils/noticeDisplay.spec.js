import { describe, it, expect } from 'vitest'
import { createI18n } from 'vue-i18n'
import zhCN from '@/locales/zh-CN'
import enUS from '@/locales/en-US'
import {
  NOTICE_TEMPLATE_KEYS,
  isSystemNotice,
  renderNoticeText,
  noticeTarget,
  needsCircleLookup,
  reportedQueueTarget,
  REVIEW_NOTICE_TYPES,
  FOLLOW_NOTICE_TYPES,
  NOTICE_TYPE,
  isFollowNotice,
  followNoticeActorId
} from '@/utils/noticeDisplay'

// 用真实文案编译，顺带校验模板 key 与占位符对得上
const createT = (locale) => createI18n({
  legacy: false,
  locale,
  messages: { 'zh-CN': zhCN, 'en-US': enUS }
}).global.t

const zh = createT('zh-CN')
const en = createT('en-US')

describe('noticeDisplay', () => {
  describe('renderNoticeText', () => {
    it('1–6：带 actor 时渲染触发人与摘要', () => {
      const actor = { username: 'Alice' }
      expect(renderNoticeText({ notice_type: 1, actor, snippet: '标题' }, zh)).toBe('Alice 赞了你的帖子《标题》')
      expect(renderNoticeText({ notice_type: 2, actor, snippet: '评论' }, zh)).toBe('Alice 赞了你的评论「评论」')
      expect(renderNoticeText({ notice_type: 3, actor, snippet: '标题' }, zh)).toBe('Alice 收藏了你的帖子《标题》')
      expect(renderNoticeText({ notice_type: 4, actor, snippet: '内容' }, zh)).toBe('Alice 评论了你的帖子：内容')
      expect(renderNoticeText({ notice_type: 5, actor, snippet: '内容' }, zh)).toBe('Alice 回复了你的评论：内容')
      expect(renderNoticeText({ notice_type: 6, actor, snippet: '内容' }, zh)).toBe('Alice 在帖子/评论中提到了你：内容')
    })

    it('actor 缺省时兜底为「有人」/ Someone', () => {
      expect(renderNoticeText({ notice_type: 1, snippet: '标题' }, zh)).toBe('有人 赞了你的帖子《标题》')
      expect(renderNoticeText({ notice_type: 1, actor: {}, snippet: 'T' }, en)).toBe('Someone liked your post "T"')
    })

    it('7：帖子审核通过（无 actor）', () => {
      expect(renderNoticeText({ notice_type: 7, snippet: '我的帖子' }, zh)).toBe('你的帖子《我的帖子》已通过审核')
      expect(renderNoticeText({ notice_type: 7, snippet: 'My post' }, en)).toBe('Your post "My post" has been approved')
    })

    it('8：帖子审核驳回（无 actor）', () => {
      expect(renderNoticeText({ notice_type: 8, snippet: '我的帖子' }, zh)).toBe('你的帖子《我的帖子》未通过审核')
      expect(renderNoticeText({ notice_type: 8, snippet: 'My post' }, en)).toBe('Your post "My post" didn\'t pass review')
    })

    it('snippet 缺省时渲染为空串而不是 undefined', () => {
      expect(renderNoticeText({ notice_type: 7 }, zh)).toBe('你的帖子《》已通过审核')
    })

    it('未知类型兜底显示 snippet，snippet 缺省时为空串', () => {
      expect(renderNoticeText({ notice_type: 99, snippet: '原文' }, zh)).toBe('原文')
      expect(renderNoticeText({ notice_type: 99 }, zh)).toBe('')
    })

    it('9 / 11：举报、下架（无 actor）', () => {
      expect(renderNoticeText({ notice_type: 9, snippet: '帖' }, zh)).toBe('本圈帖子《帖》被举报，请及时复核')
      expect(renderNoticeText({ notice_type: 11, snippet: '帖' }, zh))
        .toBe('你的帖子《帖》被举报，经圈主 / 管理员复核已下架')
      expect(renderNoticeText({ notice_type: 9, snippet: 'P' }, en)).toBe('A post in your circle, "P", was reported. Please review it.')
    })

    it('10：疑似自伤，actor 为帖子作者', () => {
      expect(renderNoticeText({ notice_type: 10, actor: { username: 'Bob' }, snippet: '帖' }, zh))
        .toBe('本圈 Bob 的帖子《帖》疑似有自伤 / 轻生倾向，请关注')
    })

    it('1–11 都有模板，且 zh / en 都能编译', () => {
      for (let type = 1; type <= 11; type++) {
        expect(NOTICE_TEMPLATE_KEYS[type]).toBeTruthy()
        for (const t of [zh, en]) {
          expect(renderNoticeText({ notice_type: type, snippet: 'S' }, t)).toContain('S')
        }
      }
    })
  })

  describe('12：被关注', () => {
    it('文案「{actor} 关注了你」，不拼 snippet', () => {
      expect(renderNoticeText({ notice_type: 12, actor: { id: 'u1', username: 'Bob' }, snippet: '' }, zh)).toBe('Bob 关注了你')
      expect(renderNoticeText({ notice_type: 12, actor: { id: 'u1', username: 'Bob' }, snippet: '' }, en)).toBe('Bob followed you')
    })

    it('actor 缺省时兜底「有人关注了你」，且不可跳转', () => {
      const item = { notice_type: 12, snippet: '' }
      expect(renderNoticeText(item, zh)).toBe('有人关注了你')
      expect(renderNoticeText(item, en)).toBe('Someone followed you')
      expect(noticeTarget(item)).toBeNull()
      expect(followNoticeActorId(item)).toBeNull()
      expect(needsCircleLookup(item)).toBe(false)
    })

    it('有 actor 时跳关注者主页，不走帖子详情', () => {
      const item = { notice_type: 12, actor: { id: 'u1', username: 'Bob' } }
      expect(noticeTarget(item)).toEqual({ name: 'user-detail', params: { id: 'u1' } })
      expect(followNoticeActorId(item)).toBe('u1')
      expect(isFollowNotice(item)).toBe(true)
      expect(isSystemNotice(item)).toBe(false)
    })

    it('其它类型不算关注通知；「关注」tab 只过滤 12', () => {
      expect(isFollowNotice({ notice_type: 1, actor: { id: 'u1' } })).toBe(false)
      expect(followNoticeActorId({ notice_type: 1, actor: { id: 'u1' } })).toBeNull()
      expect(NOTICE_TYPE.FOLLOW).toBe(12)
      expect(FOLLOW_NOTICE_TYPES).toBe('12')
    })
  })

  describe('isSystemNotice', () => {
    it('7/8/9/11 为系统通知，其余类型（含 actor 缺省的 1–6、带作者的 10）不是', () => {
      for (const type of [7, 8, 9, 11]) expect(isSystemNotice({ notice_type: type })).toBe(true)
      expect(isSystemNotice({ notice_type: 10 })).toBe(false)
      expect(isSystemNotice({ notice_type: 1 })).toBe(false)
      expect(isSystemNotice({ notice_type: 6 })).toBe(false)
      expect(isSystemNotice({ notice_type: 99 })).toBe(false)
    })
  })

  describe('noticeTarget', () => {
    it('7 / 10 跳帖子详情，不带 comment_id', () => {
      expect(noticeTarget({ notice_type: 7, post_id: 'p1' })).toEqual({ path: '/post/p1', query: {} })
      expect(noticeTarget({ notice_type: 10, post_id: 'p3' })).toEqual({ path: '/post/p3', query: {} })
    })

    it('8 / 11 跳「我的帖子」（帖子对他人不可见）', () => {
      const myPosts = { path: '/profile', query: { tab: 'posts' } }
      expect(noticeTarget({ notice_type: 8, post_id: 'p2' })).toEqual(myPosts)
      expect(noticeTarget({ notice_type: 11, post_id: 'p2' })).toEqual(myPosts)
    })

    it('9 带 circle_id 直达审核页被举报 tab 并定位；缺 circle_id 返回 null 等点击时解析', () => {
      expect(noticeTarget({ notice_type: 9, post_id: 'p1', circle_id: 'c1' }))
        .toEqual({ path: '/circle/c1/moderation', query: { tab: 'reported', focus: 'p1' } })
      expect(needsCircleLookup({ notice_type: 9, post_id: 'p1', circle_id: 'c1' })).toBe(false)
      expect(noticeTarget({ notice_type: 9, post_id: 'p1' })).toBeNull()
      expect(needsCircleLookup({ notice_type: 9, post_id: 'p1' })).toBe(true)
      expect(needsCircleLookup({ notice_type: 7, post_id: 'p1' })).toBe(false)
      expect(needsCircleLookup({ notice_type: 9 })).toBe(false)
    })

    it('reportedQueueTarget 不带帖子时不加 focus', () => {
      expect(reportedQueueTarget('c1')).toEqual({ path: '/circle/c1/moderation', query: { tab: 'reported' } })
    })

    it('「审核」tab 过滤值只含数字 7–11', () => {
      expect(REVIEW_NOTICE_TYPES).toBe('7,8,9,10,11')
    })

    it('带 comment_id 时附带定位参数', () => {
      expect(noticeTarget({ notice_type: 5, post_id: 'p1', comment_id: 'c1' }))
        .toEqual({ path: '/post/p1', query: { comment_id: 'c1' } })
    })

    it('无 post_id 时返回 null，降级为普通块', () => {
      expect(noticeTarget({ notice_type: 7 })).toBeNull()
    })
  })
})
