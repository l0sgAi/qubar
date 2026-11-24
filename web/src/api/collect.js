import request from '@/utils/request'

/**
 * 收藏 / 取消收藏
 * 带 action 时为「设为该状态」：重复请求只生效一次，超时 / 503 后可安全重试；
 * 不带 action 时按服务端真实状态取反（兼容旧写法，不要自动重试）。
 * @param {Object} data
 * @param {string} data.post_id - 帖子ID(UUIDv7，必填)
 * @param {'collect'|'uncollect'} [data.action] - 期望状态（推荐始终传）
 * @returns {Promise} data: { is_collected: boolean, post_id: string }
 *   —— is_collected 为操作后的最终状态，以服务端为准；已处于期望状态时同样返回 200
 * 错误：400 参数非法 / 404 帖子不存在或已删除 / 503 事件投递失败（服务端已回滚，可重试）/ 500 其它
 */
export function toggleCollect(data) {
  return request({
    url: '/collect/toggle',
    method: 'post',
    data
  })
}

/**
 * 我的收藏列表（按收藏时间倒序，游标分页）
 * 失效帖（被删除/封禁）由后端静默过滤，列表长度可能小于 size 属正常。
 * @param {Object} params
 * @param {number} params.size - 每页数量，默认20，<=0 或 >100 后端回退为20
 * @param {string} [params.search_after] - 上一页响应游标，原样透传（首页不传 / 空串）
 * @param {string} [params.keyword] - 关键字（服务端匹配标题/摘要/正文/作者等）
 * @returns {Promise} data: { posts: PostListItem[], total: number, size: number, search_after: string }
 *   —— search_after 为空串表示已到末页
 */
export function getCollectedPosts(params) {
  return request({
    url: '/collect/posts',
    method: 'get',
    params
  })
}
