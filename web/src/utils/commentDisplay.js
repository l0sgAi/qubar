// 评论展示用的纯函数：CommentList / CommentReplies / CommentItem 共用

// 获取用户ID（兼容多种字段名）；注意不能回退到 item.id——那是评论ID，
// 作者信息缺失时会错误地链到 /user/<评论ID>
export const getUserId = (item) => {
  return item.author_id || item.user_id || item.userId || item.authorId || null
}

// 作者名统一用 SmartLink 渲染为真实链接；无 id 时降级为纯文本块
export const userUrl = (userId) => (userId ? `/user/${userId}` : null)

export const getCommentImages = (item) => {
  if (!item.extra_data) return []
  try {
    const data = typeof item.extra_data === 'string' ? JSON.parse(item.extra_data) : item.extra_data
    return data?.images || []
  } catch {
    return []
  }
}

// 获取被回复用户的名称
export const getReplyToName = (reply, replies) => {
  // 如果后端已经返回 reply_to_name，直接使用
  if (reply.reply_to_name) {
    return reply.reply_to_name
  }
  // 如果有 reply_to_id 且非0，从回复列表中查找对应的用户名
  if (reply.reply_to_id && replies) {
    const targetReply = replies.find(r => r.id === reply.reply_to_id)
    return targetReply?.author_name
  }
  return null
}

// 获取被回复用户的ID
export const getReplyToUserId = (reply, replies) => {
  // 优先使用后端返回的 reply_to_user_id
  if (reply.reply_to_user_id) {
    return reply.reply_to_user_id
  }
  // 如果有 reply_to_id 且非0，从回复列表中查找对应的用户ID
  if (reply.reply_to_id && replies) {
    const targetReply = replies.find(r => r.id === reply.reply_to_id)
    return targetReply?.author_id
  }
  return null
}
