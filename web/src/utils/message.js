/**
 * 私信（后端 v0.9.0）的常量与纯函数，不含文案（文案在 locales 的 message.*）。
 * 接口封装见 api/message.js；实时事件见 composables/useUnreadNotice.js。
 */
import {
  isBadRequestError,
  isForbiddenError,
  isNotFoundError,
  isRateLimitedError,
  isRetryableError,
  isUnauthorizedError,
  errorMessageIncludes
} from '@/utils/apiError'

// 谁可以给我发私信（GET/PUT /user/settings 的 dm_permission；与后端枚举一致，勿改数值）
export const DM_PERMISSION = Object.freeze({
  EVERYONE: 0,
  FOLLOWING: 1, // 仅我关注的人
  MUTUAL: 2, // 仅互相关注
  CLOSED: 3 // 关闭
})

// dm_permission → message.permission.* 文案 key 后缀（设置单选，按展示顺序）
export const DM_PERMISSION_OPTIONS = Object.freeze([
  { value: DM_PERMISSION.EVERYONE, key: 'everyone' },
  { value: DM_PERMISSION.FOLLOWING, key: 'following' },
  { value: DM_PERMISSION.MUTUAL, key: 'mutual' },
  { value: DM_PERMISSION.CLOSED, key: 'closed' }
])

// 正文上限（按字符计，与后端一致：中文 / emoji 各算 1）
export const DM_CONTENT_MAX = 2000
// 未关注时每个会话最多发送的条数（仅用于提示，以 remaining_count 为准）
export const DM_UNFOLLOWED_LIMIT = 5
// 列表页大小：默认 20，后端上限 50
export const DM_PAGE_SIZE = 20
// SSE dm-message 与会话列表 last_message 的正文截断长度
export const DM_PREVIEW_LENGTH = 100
// 消息类型：当前只有文本
export const DM_MSG_TYPE_TEXT = 1

/**
 * 不能发送的原因。前 6 个是 GET /message/conversations/with/:user_id 的 cannot_send_reason 枚举（后端值），
 * 其余是发送接口才有的错误。值 → message.error.* 文案 key 后缀。
 */
export const DM_REASON = Object.freeze({
  RECIPIENT_NOT_FOUND: 'recipient_not_found',
  RECIPIENT_UNAVAILABLE: 'recipient_unavailable',
  FOLLOWING_ONLY: 'following_only',
  MUTUAL_ONLY: 'mutual_only',
  DM_CLOSED: 'dm_closed',
  DM_LIMIT_REACHED: 'dm_limit_reached',
  // 以下仅发送失败时出现
  SENDER_DISABLED: 'sender_disabled',
  CONVERSATION_NOT_FOUND: 'conversation_not_found',
  RATE_LIMITED: 'rate_limited',
  CONTENT_EMPTY: 'content_empty',
  CONTENT_TOO_LONG: 'content_too_long',
  NETWORK: 'network',
  UNKNOWN: 'unknown'
})

const REASON_KEYS = {
  [DM_REASON.RECIPIENT_NOT_FOUND]: 'recipientNotFound',
  [DM_REASON.RECIPIENT_UNAVAILABLE]: 'recipientUnavailable',
  [DM_REASON.FOLLOWING_ONLY]: 'followingOnly',
  [DM_REASON.MUTUAL_ONLY]: 'mutualOnly',
  [DM_REASON.DM_CLOSED]: 'dmClosed',
  [DM_REASON.DM_LIMIT_REACHED]: 'dmLimitReached',
  [DM_REASON.SENDER_DISABLED]: 'senderDisabled',
  [DM_REASON.CONVERSATION_NOT_FOUND]: 'conversationNotFound',
  [DM_REASON.RATE_LIMITED]: 'rateLimited',
  [DM_REASON.CONTENT_EMPTY]: 'contentEmpty',
  [DM_REASON.CONTENT_TOO_LONG]: 'contentTooLong',
  [DM_REASON.NETWORK]: 'network',
  [DM_REASON.UNKNOWN]: 'unknown'
}

/** 原因 → i18n key（message.error.*）；未知原因（后端新增枚举）兜底 unknown。contentTooLong 需传 { max } */
export function dmReasonKey(reason) {
  return `message.error.${REASON_KEYS[reason] || REASON_KEYS[DM_REASON.UNKNOWN]}`
}

// 403 只能靠英文 message 区分（几个 403 的 code 都是 203）
const FORBIDDEN_MESSAGES = [
  ['cannot receive private messages', DM_REASON.RECIPIENT_UNAVAILABLE],
  ['dm limit reached', DM_REASON.DM_LIMIT_REACHED],
  ['your account cannot send', DM_REASON.SENDER_DISABLED],
  ['from people they follow', DM_REASON.FOLLOWING_ONLY],
  ['from mutual follows', DM_REASON.MUTUAL_ONLY],
  ['turned off private messages', DM_REASON.DM_CLOSED]
]

/**
 * POST /message/send（及会话相关接口）失败 → DM_REASON。
 * 调用方据此决定：置灰输入框（权限 / 5 条限制 / 对方不存在）、提示不重试（429）、用同一 client_msg_id 重试（network）。
 */
export function dmSendErrorReason(error) {
  if (!error) return DM_REASON.UNKNOWN
  if (isRateLimitedError(error)) return DM_REASON.RATE_LIMITED
  if (isForbiddenError(error)) {
    const hit = FORBIDDEN_MESSAGES.find(([fragment]) => errorMessageIncludes(error, fragment))
    return hit ? hit[1] : DM_REASON.UNKNOWN
  }
  if (isNotFoundError(error)) {
    return errorMessageIncludes(error, 'conversation')
      ? DM_REASON.CONVERSATION_NOT_FOUND
      : DM_REASON.RECIPIENT_NOT_FOUND
  }
  if (isBadRequestError(error)) {
    if (errorMessageIncludes(error, 'too long')) return DM_REASON.CONTENT_TOO_LONG
    if (errorMessageIncludes(error, 'empty')) return DM_REASON.CONTENT_EMPTY
    return DM_REASON.UNKNOWN
  }
  if (isUnauthorizedError(error)) return DM_REASON.UNKNOWN
  if (isRetryableError(error)) return DM_REASON.NETWORK
  return DM_REASON.UNKNOWN
}

/** 发送失败 → i18n key */
export function dmSendErrorKey(error) {
  return dmReasonKey(dmSendErrorReason(error))
}

/** 发送失败后是否该用同一个 client_msg_id 重试（超时 / 网络错误 / 5xx）；429 不自动重试 */
export function isDmSendRetryable(error) {
  return dmSendErrorReason(error) === DM_REASON.NETWORK
}

/** 发送失败后对方是否已不可发（权限 / 5 条限制 / 对方不存在或机器人）：应置灰输入框 */
export function isDmBlockedReason(reason) {
  return [
    DM_REASON.RECIPIENT_NOT_FOUND,
    DM_REASON.RECIPIENT_UNAVAILABLE,
    DM_REASON.FOLLOWING_ONLY,
    DM_REASON.MUTUAL_ONLY,
    DM_REASON.DM_CLOSED,
    DM_REASON.DM_LIMIT_REACHED,
    DM_REASON.SENDER_DISABLED
  ].includes(reason)
}

/** 字符数（按 Unicode 码点，与后端一致：emoji 算 1） */
export function dmContentLength(content) {
  return Array.from(String(content ?? '')).length
}

/**
 * 发送前校验正文：返回 null（可发送）或 DM_REASON.CONTENT_EMPTY / CONTENT_TOO_LONG。
 * 只有空白也算空；不 trim 原文（服务端原样保存，含换行）。
 */
export function validateDmContent(content) {
  const text = String(content ?? '')
  if (text.trim() === '') return DM_REASON.CONTENT_EMPTY
  if (dmContentLength(text) > DM_CONTENT_MAX) return DM_REASON.CONTENT_TOO_LONG
  return null
}

/**
 * 生成发送幂等键 client_msg_id（UUID）。每条新消息生成一次，重试时复用。
 * 非安全上下文没有 crypto.randomUUID 时用 getRandomValues 拼 v4，再不行用 Math.random（仅作幂等键，不涉及安全）。
 */
export function createClientMsgId() {
  const c = globalThis.crypto
  if (typeof c?.randomUUID === 'function') return c.randomUUID()
  const bytes = new Uint8Array(16)
  if (typeof c?.getRandomValues === 'function') c.getRandomValues(bytes)
  else for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256)
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}

/** remaining_count 是否需要提示（数字 = 受未关注 5 条限制；null / undefined = 不受限） */
export function hasDmRemainingLimit(remainingCount) {
  return typeof remainingCount === 'number' && remainingCount >= 0
}

/**
 * 合并消息并按 id 去重，返回「旧 → 新」顺序（渲染顺序）。
 * 消息 id 是 UUIDv7（同长度小写十六进制），字符串比较即时间顺序。
 * 后来的同 id 消息覆盖先前的（如 SSE 截断摘要被拉取到的全文覆盖）。
 * @param {Array} existing - 已有消息（任意顺序）
 * @param {Array} incoming - 新消息（任意顺序，如接口返回的「新 → 旧」页、SSE 单条、发送响应）
 */
export function mergeMessagesById(existing = [], incoming = []) {
  const byId = new Map()
  for (const msg of [...existing, ...incoming]) {
    if (msg?.id) byId.set(msg.id, msg)
  }
  return [...byId.values()].sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0))
}

/** 消息是否我发的 */
export function isOwnMessage(msg, myUserId) {
  return !!msg && !!myUserId && msg.sender_id === myUserId
}
