/**
 * 私信页面（会话列表 / 聊天页）用的纯函数，不含文案。
 * 接口与常量见 api/message.js、utils/message.js（工作流 A 的契约）。
 */

// 聊天页相邻两条消息间隔超过该值时插一条时间标签
export const TIME_LABEL_GAP_MS = 5 * 60 * 1000

/** SSE dm-message → MessageVO（去掉 sender / truncated，便于与拉取结果按 id 合并） */
export function toMessageVO(msg) {
  if (!msg) return null
  const { id, conversation_id, sender_id, msg_type, content, create_time } = msg
  return { id, conversation_id, sender_id, msg_type, content, create_time }
}

/** 发送响应 → MessageVO（去掉 remaining_count） */
export function sentToMessageVO(data) {
  if (!data) return null
  const { remaining_count: _ignored, ...vo } = data
  return vo
}

/**
 * 收到 dm-message 后更新会话列表：命中的会话顶到最前、更新摘要、未读 +1。
 * 没命中（新会话 / 被删除后重新出现 / 还没翻到那一页）时返回 found=false，调用方刷新第一页。
 * 同一条消息重复到达（id 不大于当前 last_message.id）保留当前顺序和未读数。
 * @returns {{ list: Array, found: boolean }}
 */
export function bumpConversation(list, msg) {
  const index = list.findIndex(c => c.conversation_id === msg?.conversation_id)
  if (index === -1) return { list, found: false }
  const conv = list[index]
  if (conv.last_message && msg.id <= conv.last_message.id) return { list, found: true }
  const updated = {
    ...conv,
    last_message: {
      id: msg.id,
      sender_id: msg.sender_id,
      msg_type: msg.msg_type,
      content: msg.content,
      create_time: msg.create_time
    },
    unread_count: (conv.unread_count || 0) + (msg.sender_id === conv.peer_id ? 1 : 0)
  }
  return { list: [updated, ...list.slice(0, index), ...list.slice(index + 1)], found: true }
}

/** 会话列表各项未读数之和（与 dm-unread 比较，判断列表是否需要刷新） */
export function sumUnread(list) {
  return list.reduce((sum, c) => sum + (c.unread_count || 0), 0)
}

/** 最后一条消息是不是我发的（单聊：发送者不是对方即为我） */
export function isLastMessageMine(conv) {
  return !!conv?.last_message && conv.last_message.sender_id !== conv.peer_id
}

/**
 * 「以下为新消息」分隔线画在哪条消息前：第一条 id 大于 lastReadId 的对方消息。
 * 在第一条之前（整页都是新消息）不画；lastReadId 为空串表示从未读过，同样按「全部是新消息」处理。
 * @param {Array} messages - 旧 → 新
 * @returns {string|null} 消息 id
 */
export function newMessagesDividerId(messages, lastReadId, peerId) {
  if (lastReadId === null || lastReadId === undefined) return null
  const index = messages.findIndex(m => m.sender_id === peerId && m.id > lastReadId)
  return index > 0 ? messages[index].id : null
}

/** 相邻消息是否需要插时间标签（第一条总是需要） */
export function needsTimeLabel(prev, cur) {
  if (!cur) return false
  if (!prev) return true
  const a = new Date(prev.create_time).getTime()
  const b = new Date(cur.create_time).getTime()
  if (Number.isNaN(a) || Number.isNaN(b)) return false
  return b - a > TIME_LABEL_GAP_MS
}

/**
 * 聊天页时间标签：今天只显示时分，今年显示月日 + 时分，更早带年份。
 * @param {string} iso - RFC 3339
 * @param {string} locale - 'zh-CN' | 'en-US'
 * @param {Date} [now]
 */
export function formatChatTime(iso, locale, now = new Date()) {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return ''
  const time = { hour: '2-digit', minute: '2-digit', hour12: false }
  if (date.toDateString() === now.toDateString()) {
    return date.toLocaleTimeString(locale, time)
  }
  const sameYear = date.getFullYear() === now.getFullYear()
  return date.toLocaleString(locale, {
    ...(sameYear ? {} : { year: 'numeric' }),
    month: 'numeric',
    day: 'numeric',
    ...time
  })
}

/** 发送重试的退避时长（毫秒）：第 n 次自动重试前等待；超过次数后交给用户手动重试 */
export const SEND_RETRY_DELAYS = Object.freeze([1000, 3000])
