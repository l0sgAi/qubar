import request from '@/utils/request'

/**
 * 点赞 / 取消点赞（帖子或评论）
 * 带 action 时为「设为该状态」：重复请求只生效一次，超时 / 503 后可安全重试；
 * 不带 action 时按服务端真实状态取反（兼容旧写法，不要自动重试）。
 * @param {Object} data
 * @param {'post'|'comment'} data.type - 目标类型
 * @param {string} data.target_id - 帖子ID 或 评论ID(UUIDv7)
 * @param {'like'|'unlike'} [data.action] - 期望状态（推荐始终传）
 * @returns {Promise} data: { is_liked: boolean, type: string, target_id: string }
 *   —— is_liked 为操作后的最终状态，以服务端为准；已处于期望状态时同样返回 200
 * 错误：400 参数非法 / 404 目标不存在或已删除 / 503 事件投递失败（服务端已回滚，可重试）/ 500 其它
 */
export function toggleLike(data) {
  return request({
    url: '/like/toggle',
    method: 'post',
    data
  })
}
