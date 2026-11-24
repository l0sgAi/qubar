import request from '@/utils/request'

/**
 * 最近浏览列表（按最近访问时间倒序，offset 分页）
 *
 * 与「我的收藏」/collect/posts 的差异：
 *  - 分页为 offset（offset + next_offset），非游标 search_after；
 *  - 末页判据为响应**省略** next_offset 字段（非空串）。
 * （keyword 关键字搜索两接口均支持，语义一致。）
 *
 * 浏览历史由服务端在 GET /post/detail/:id 时自动记录，前端无需调用任何「记录」接口。
 * 失效帖（被删除/封禁）由后端静默过滤，posts.length < size 属正常，勿据此报错。
 *
 * @param {Object} params
 * @param {number} [params.size] - 每页数量，默认20；<=0 或 >100 后端回退为20
 * @param {number} [params.offset] - 偏移量，默认0；<0 后端按0 处理
 * @param {string} [params.keyword] - 关键字（标题/摘要/正文/作者等，服务端匹配）
 * @returns {Promise} data: { posts: PostListItem[], total: number, size: number, next_offset?: number }
 *   —— next_offset 字段省略 = 已到末页；total 为 ZSET 容量（上限500），可能略大于实际可返回数
 */
export function getHistoryPosts(params) {
  return request({
    url: '/history/posts',
    method: 'get',
    params
  })
}
