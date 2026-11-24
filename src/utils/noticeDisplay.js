// 消息中心展示用的纯函数：文案渲染与跳转目标，供 Notifications.vue 使用

// 审核类通知（l0sgAi/qubar#118、#153）
export const NOTICE_TYPE = {
  POST_APPROVED: 7, // 作者：审核通过
  POST_REJECTED: 8, // 作者：审核驳回
  POST_REPORTED: 9, // 圈主 / 管理员：本圈帖子被举报
  POST_SELF_HARM: 10, // 圈主 / 管理员：本圈帖子疑似自伤（actor = 帖子作者）
  POST_REMOVED: 11, // 作者：被举报经复核下架
  FOLLOW: 12 // 被关注（l0sgAi/qubar#219）：actor = 关注者，无 post_id / comment_id，snippet 为空串
}

// 消息中心「审核」tab 的 type 过滤值（后端只接受数字 1–12，逗号分隔）
export const REVIEW_NOTICE_TYPES = '7,8,9,10,11'

// 消息中心「关注」tab 的 type 过滤值
export const FOLLOW_NOTICE_TYPES = '12'

// 文案模板按 notice_type 映射到 notice.templates.*
export const NOTICE_TEMPLATE_KEYS = {
  1: 'likePost',
  2: 'likeComment',
  3: 'collectPost',
  4: 'commentPost',
  5: 'replyComment',
  6: 'mention',
  7: 'postApproved',
  8: 'postRejected',
  9: 'postReported',
  10: 'postSelfHarm',
  11: 'postRemoved',
  12: 'follow'
}

// 系统类通知：后端恒不返回 actor（不暴露审核人 / 举报人 / 处理人），头像用系统图标而不是首字母兜底。
// 10 带 actor（帖子作者），按普通通知渲染头像
export const SYSTEM_NOTICE_TYPES = [
  NOTICE_TYPE.POST_APPROVED,
  NOTICE_TYPE.POST_REJECTED,
  NOTICE_TYPE.POST_REPORTED,
  NOTICE_TYPE.POST_REMOVED
]

export const isSystemNotice = (item) => SYSTEM_NOTICE_TYPES.includes(item.notice_type)

// actor 缺省（用户注销等降级场景）兜底为「有人」；未知类型直接显示 snippet
export const renderNoticeText = (item, t) => {
  const key = NOTICE_TEMPLATE_KEYS[item.notice_type]
  if (!key) return item.snippet || ''
  // 12 无 actor：整句兜底「有人关注了你」（中文模板里 actor 后有空格，不能直接代入「有人」）
  if (isFollowNotice(item) && !item.actor?.username) return t('notice.templates.followAnonymous')
  const actor = item.actor?.username || t('notice.someone')
  return t(`notice.templates.${key}`, { actor, snippet: item.snippet || '' })
}

// 「我的帖子」：驳回 / 下架的帖子对他人不可见，作者在这里看到状态
export const MY_POSTS_TARGET = { path: '/profile', query: { tab: 'posts' } }

// 「我管理的圈子」：举报通知找不到帖子所属圈子时的兜底落点
export const MANAGED_CIRCLES_TARGET = { path: '/circle/managed' }

// 圈子管理 → 内容审核 →「被举报」tab，focus 定位该帖子（见 CircleModeration.vue）
export const reportedQueueTarget = (circleId, postId) => ({
  path: `/circle/${circleId}/moderation`,
  query: postId ? { tab: 'reported', focus: postId } : { tab: 'reported' }
})

// 类型 9 缺 circle_id（后端 l0sgAi/qubar#156 上线前）：没有可直接渲染的链接，点击时再查帖子详情拿圈子
export const needsCircleLookup = (item) =>
  item.notice_type === NOTICE_TYPE.POST_REPORTED && !!item.post_id && !item.circle_id

// 被关注通知（12）：有 actor 时可跳关注者主页、可显示回关按钮；actor 缺省（已注销 / 禁用）时文案兜底「有人关注了你」且不可点
export const isFollowNotice = (item) => item.notice_type === NOTICE_TYPE.FOLLOW
export const followNoticeActorId = (item) => (isFollowNotice(item) && item.actor?.id) || null

// 跳转目标；无目标或需要异步解析时返回 null（降级为普通块，点击由页面处理）
// - 12：关注者主页（actor.id），actor 缺省时不可点
// - 9：审核页被举报 tab 并定位（优先用 item.circle_id）
// - 8 / 11：「我的帖子」（帖子对他人不可见）
// - 其余（含 7、10）：帖子详情，有 comment_id 带上定位参数
export const noticeTarget = (item) => {
  if (isFollowNotice(item)) {
    const actorId = followNoticeActorId(item)
    return actorId ? { name: 'user-detail', params: { id: actorId } } : null
  }
  if (!item.post_id) return null
  switch (item.notice_type) {
    case NOTICE_TYPE.POST_REPORTED:
      return item.circle_id ? reportedQueueTarget(item.circle_id, item.post_id) : null
    case NOTICE_TYPE.POST_REJECTED:
    case NOTICE_TYPE.POST_REMOVED:
      return MY_POSTS_TARGET
    default:
      return {
        path: `/post/${item.post_id}`,
        query: item.comment_id ? { comment_id: item.comment_id } : {}
      }
  }
}
