// @提及 渲染期 username→uuid 缓存：正文只存纯文本（后端不解析 @），链接化时
// 用缓存把 @用户名 映射回 uuid 再建链。缓存只有两个来源：编辑器选人成功时
// seedUsers 回灌、后端随内容回传 mentions 时 seedContentMentions 回灌。
// 不做搜索反查——圈内机器人在全站搜索里不可见，反查必然失败还白耗 ES 查询；
// 缓存未命中的 @token 由渲染侧（mentionDom）保持纯文本。缓存为模块级，整个会话共享。

// 单条内容最多生效 10 人（与 notice.mention.limitTip 文案、MentionPicker 拦截保持同步）
export const MAX_MENTIONS = 10

const store = new Map()   // lower(用户名) -> uuid（只含已确认的映射）
// 原样大小写的已知用户名（含空格等非常规字符）：供 getMentionFullRe 构建
// 已知名优先匹配。seedUsers 回灌时登记。
const knownNames = new Set()

// 选人成功即回灌缓存（编辑器内联弹窗 / MentionPicker 共用）
export const seedUsers = (users = []) => {
  users.forEach((u) => {
    if (u?.id && u.username) {
      store.set(String(u.username).toLowerCase(), u.id)
      knownNames.add(String(u.username))
    }
  })
}

// 已知用户名快照（原样大小写），供已知名优先匹配
export const knownUsernames = () => [...knownNames]

// 从内容载体批量回灌提及用户（后端在帖子/评论详情回传 mentions: [{id, username}]）。
// 渲染链接化前调用 → store 精确命中，零搜索请求建链；旧内容无 mentions 时静默跳过，
// 对应 @token 保持纯文本。
export const seedContentMentions = (items = []) => {
  const arr = Array.isArray(items) ? items : []
  arr.forEach((it) => {
    if (Array.isArray(it?.mentions) && it.mentions.length) seedUsers(it.mentions)
  })
}

// 立即取已知映射：uuid / undefined(未知，渲染侧按纯文本处理)
export const peekUserId = username => store.get(String(username || '').toLowerCase())
