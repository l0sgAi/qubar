import request from '@/utils/request'

// 私信（后端 v0.9.0，l0sgAi/qubar#238 及后续）。对接文档：后端仓 docs/api/message-api.md。
// 全部接口需要登录。列表接口游标分页：{ cursor, size }（size 默认 20、最大 50），结束看 has_more。
// 常量、发送校验与错误判断见 utils/message.js；实时事件（dm-unread / dm-message）见 composables/useUnreadNotice.js。

/**
 * 发送私信。第一次给某人发消息时自动建会话。
 * 每条新消息生成一个 client_msg_id（utils/message.js 的 createClientMsgId）；
 * 超时 / 网络错误 / 5xx 用同一个 client_msg_id 重试，服务端保证只发一条（返回原消息）。
 * @param {Object} data
 * @param {string} data.receiver_id - 接收方用户 ID
 * @param {string} data.content - 正文，1–2000 字符，不能全空白
 * @param {string} [data.client_msg_id] - 幂等键，1–64 位字母 / 数字 / - / _
 * @returns {Promise} data: MessageVO + remaining_count
 *   MessageVO: { id, conversation_id, sender_id, msg_type, content, create_time }
 *   remaining_count: number | null（null = 不受未关注 5 条限制）
 */
export function sendMessage(data) {
  return request({
    url: '/message/send',
    method: 'post',
    data
  })
}

/**
 * 会话列表（按最后一条消息时间倒序）
 * @param {Object} [params] - { cursor, size }
 * @returns {Promise} data: { items, has_more, next_cursor }
 *   items[]: { conversation_id, type, peer_id, peer: {id, username, avatar_url} | null, last_message, unread_count }
 *   （peer 为 null = 对方已禁用 / 删除；last_message.content 只有前 100 字）
 */
export function getConversations(params) {
  return request({
    url: '/message/conversations',
    method: 'get',
    params
  })
}

/**
 * 按对方用户 ID 查会话 + 我现在能否给对方发私信（只读，不创建会话、不清未读）
 * @param {string} userId - 对方用户 ID
 * @returns {Promise} data: { conversation: 会话列表项 | null, can_send, cannot_send_reason: string | null }
 *   （删除过且之后没有新消息时 conversation.last_message 为 null）
 */
export function getConversationWith(userId) {
  return request({
    url: `/message/conversations/with/${userId}`,
    method: 'get'
  })
}

/**
 * 消息历史：第一页是最新的 size 条，按「新 → 旧」返回（渲染时反转），next_cursor 往更旧翻。
 * @param {string} conversationId
 * @param {Object} [params] - { cursor, size }
 * @returns {Promise} data: { items: MessageVO[], has_more, next_cursor, last_read_message_id }
 *   （last_read_message_id 空串 = 从未读过；在调 markConversationRead 之前读取，用来画「以下为新消息」）
 */
export function getConversationMessages(conversationId, params) {
  return request({
    url: `/message/conversations/${conversationId}/messages`,
    method: 'get',
    params
  })
}

/**
 * 标记会话已读（清零我在该会话的未读，幂等）
 * @param {string} conversationId
 * @returns {Promise} data 为 null
 */
export function markConversationRead(conversationId) {
  return request({
    url: `/message/conversations/${conversationId}/read`,
    method: 'post'
  })
}

/**
 * 删除会话（只对自己隐藏，未读清零；对方再发消息时重新出现，只显示删除之后的消息）
 * @param {string} conversationId
 * @returns {Promise} data 为 null
 */
export function deleteConversation(conversationId) {
  return request({
    url: `/message/conversations/${conversationId}`,
    method: 'delete'
  })
}

/**
 * 私信未读总数（与 SSE dm-unread 同源同值；SSE 正常时无需调用）
 * @returns {Promise} data: { unread_count }
 */
export function getDmUnreadCount() {
  return request({
    url: '/message/unread-count',
    method: 'get'
  })
}
