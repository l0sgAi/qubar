import request from '@/utils/request'

/**
 * 获取圈子列表（用于发帖时选择圈子）
 * @param {Object} params - 查询参数
 * @param {string} params.keyword - 搜索关键词
 * @param {number} params.page - 页码
 * @param {number} params.page_size - 每页数量
 * @returns {Promise}
 */
export function getCircles(params) {
  return request({
    url: '/circle/list',
    method: 'get',
    params
  })
}

/**
 * 获取用户已加入的圈子列表（用于发帖时选择圈子）
 * @param {Object} params - 查询参数
 * 列表项：id / name / avatar_url / member_count / slug（无则省略）/ role（30 圈主 / 20 管理员 / 10 成员）
 * / member_status（1 正常 / 2 禁言）/ mute_end_time（RFC3339，仅禁言生效中返回）
 * @param {Object} params - 查询参数
 * @param {string} params.keyword - 搜索关键词（可选）
 * @param {number} params.role - 角色筛选：30 / 20 / 10；0 或不传 = 全部（可选，其他值 400）
 * @param {number} params.size - 每页数量，默认20，最大100
 * @param {string} params.search_after - 上一页响应返回的游标（原样透传，可选）；
 *   keyword / role 变化后必须丢弃旧游标，否则返回 400 Invalid search_after parameter
 * @returns {Promise}
 */
export function getMyCircles(params) {
  return request({
    url: '/circle/my',
    method: 'get',
    params
  })
}

/**
 * 获取近期活跃圈子列表（按近 7 天发帖数降序）
 * @param {Object} params - 查询参数
 * @param {number} params.size - 每页数量，默认20，1-100，越界回落20
 * @param {number} params.offset - 0 基偏移量，默认0，负数回落0
 * @returns {Promise}
 */
export function getActiveCircles(params) {
  return request({
    url: '/circle/active',
    method: 'get',
    params
  })
}

/**
 * 获取随机圈子列表（侧栏随机推荐，每次请求返回一批随机结果，无分页）
 * 响应结构与 /circle/active 完全一致；recent_post_count 恒为 0，忽略即可。
 * 无需登录，访客可读。
 * @param {Object} params - 查询参数
 * @param {number} params.size - 返回数量，默认20，<=0 或 >100 时后端回退为20
 * @returns {Promise}
 */
export function getRandomCircles(params) {
  return request({
    url: '/circle/random',
    method: 'get',
    params
  })
}

/**
 * 获取指定用户已加入的圈子列表（查看他人主页）
 * 与 /circle/my 的区别：目标用户来自 user_id 查询参数（任意已登录用户均可查看他人加入的圈子）。
 * 响应结构与 /circle/my 一致（circles / total / size / search_after，搜索模式额外可能含 truncated）。
 * 注意：搜索模式（keyword 非空）下 total 恒为 0，前端不应展示总数。
 * 列表项同 /circle/my（含 slug / role），但不返回 member_status / mute_end_time（禁言属个人隐私）。
 * @param {string} userId - 目标用户 ID（UUIDv7，必填）
 * @param {Object} params - 查询参数
 * @param {string} params.keyword - 关键字，模糊匹配圈子 name（权重×3）与 description（权重×1）（可选）
 * @param {number} params.role - 角色筛选：30 / 20 / 10；0 或不传 = 全部（可选）
 * @param {number} params.size - 每页数量，默认20，<=0 或 >100 时后端回退为20
 * @param {string} params.search_after - 上一页响应返回的游标（base64 不透明串，原样透传，可选）
 * @returns {Promise}
 */
export function getUserCircles(userId, params) {
  return request({
    url: '/circle/user',
    method: 'get',
    params: { user_id: userId, ...params }
  })
}

/**
 * 创建帖子
 * @param {Object} data - 帖子数据
 * @param {string} data.circle_id - 所属圈子ID(UUIDv7)
 * @param {string} data.title - 标题（必填）
 * @param {string} data.summary - 摘要/关键词（可选）
 * @param {string} data.content - 正文内容（Markdown格式）
 * @param {Object} data.media_extra - 媒体扩展信息
 * @returns {Promise<{ data: string }>} data 只有新帖 postID，不带 status。
 *   发帖状态由服务端决定（请求体里的 status 会被忽略）：开启审核时新帖先是 2（审核中），
 *   由 AI 异步分流为 1（已发布）/ 3（驳回），拿不准的可能一直停在 2 等人工处理；
 *   实际状态以 getPostDetail 返回的 status 为准
 */
export function createPost(data) {
  return request({
    url: '/post/create',
    method: 'post',
    data
  })
}

/**
 * 搜索帖子列表
 * @param {Object} params - 查询参数
 * @param {string} params.keyword - 搜索关键词（可选）
 * @param {string} params.circle_id - 圈子ID（可选，UUIDv7）
 * @param {number} params.size - 每页数量，默认20
 * @param {string} params.search_after - 深度分页参数（JSON字符串，可选）
 * @returns {Promise}
 */
export function searchPosts(params) {
  return request({
    url: '/post/list',
    method: 'get',
    params
  })
}

/**
 * 获取当前登录用户自己的帖子列表
 * 与 /post/list 的区别：仅返回本人帖子，且不过滤 status（草稿/审核/已发布/拒绝/封禁均可见），
 * 关键字匹配额外支持 fuzziness:AUTO（容错拼写错误）。
 * @param {Object} params - 查询参数
 * @param {string} params.keyword - 关键字，模糊匹配 title（权重×3）与 summary（权重×1），为空时返回全部（可选）
 * @param {number} params.size - 每页数量，默认20，<=0 或 >100 时后端回退为20
 * @param {string} params.search_after - 上一页响应返回的游标（JSON数组字符串，原样透传，可选）
 * @returns {Promise}
 */
export function getMyPosts(params) {
  return request({
    url: '/post/my',
    method: 'get',
    params
  })
}

/**
 * 获取指定用户的已发布帖子列表（查看他人主页）
 * 仅返回 status=1（已发布）；响应结构与 /post/my 完全一致，列表组件可直接复用。
 * @param {string} userId - 目标用户 ID（UUIDv7）
 * @param {Object} params - 查询参数
 * @param {string} params.keyword - 关键字，模糊匹配 title（权重×3）与 summary（权重×1）（可选）
 * @param {number} params.size - 每页数量，默认20，<=0 或 >100 时后端回退为20
 * @param {string} params.search_after - 上一页响应返回的游标（JSON数组字符串，原样透传，可选）
 * @returns {Promise}
 */
export function getUserPosts(userId, params) {
  return request({
    url: `/post/user/${userId}`,
    method: 'get',
    params
  })
}

/**
 * 获取帖子详情
 * @param {string} id - 帖子ID(UUIDv7)
 * @returns {Promise<{ data: Object }>} data 含 status（1 已发布 / 2 审核中 / 3 驳回 / 4 屏蔽，
 *   见 POST_STATUS）与 author_id。作者能看自己任何状态的帖子；
 *   其他人（含访客）访问非 1 的帖子一律 reject：HTTP 404、信封 code=204、message `Post not found`，
 *   与帖子不存在不作区分
 */
export function getPostDetail(id) {
  return request({
    url: `/post/detail/${id}`,
    method: 'get'
  })
}

/**
 * 首页信息流（4 个 tab 共用同一端点，通过 tab 参数切换）
 * 返回统一的 PostItem（含 view/like/comment/collect_count 实时计数与 is_liked/is_collected），
 * 但翻页机制分两套：
 *   - recommend：候选池 offset + pool_token 翻页；响应含 pool_token / pool_refreshed
 *   - hot / latest / following：search_after 游标翻页；响应含 search_after
 * @param {Object} params - 查询参数
 * @param {string} params.tab - 必填：recommend | hot | latest | following
 * @param {number} params.size - 每页条数，默认20，范围 1~100
 * @param {number} params.offset - 仅 recommend：候选池偏移（首页为 0，翻页累加）
 * @param {string} params.pool_token - 仅 recommend：上次返回的池版本 token，翻页原样回传
 * @param {string} params.search_after - 仅 hot/latest/following：上次返回的游标（JSON 数组字符串，原样透传，axios 自动 URL-encode）
 * @returns {Promise} data 为 FeedPage：{ posts, pool_token?, search_after?, has_more, pool_refreshed? }
 */
export function getHomeFeed(params) {
  return request({
    url: '/post/home',
    method: 'get',
    params
  })
}

/**
 * 举报帖子（需登录；与 moderation.enabled 无关，始终可用）
 * 只能举报已发布（status=1）且不是自己的帖子；同一帖子每人只能举报一次（处理后也不能再举报）；
 * 每人 24 小时内最多举报 moderation.report_daily_limit 次（默认 20）。举报人对作者不可见。
 * 文档：后端 docs/api/post-moderation-review-api.md 第六、九节
 * @param {Object} data - 请求体
 * @param {string} data.post_id - 被举报帖子 ID（UUIDv7）
 * @param {number} data.reason_type - 举报理由 1–8（见 src/constants/report.js 的 REPORT_REASON_TYPES）
 * @param {string} [data.reason] - 补充说明（可选，≤200 字符，见 REPORT_REASON_MAX）
 * @returns {Promise<{ data: null }>} 失败时 reject（error.status 为 HTTP 状态，error.code 为信封 code）：
 *   400 / 201 请求非法、reason_type 越界、举报自己的帖子（三者只能靠英文 message 区分，前端统一兜底）；
 *   404 / 204 帖子不存在或未发布；409 / 207 已举报过；429 / 208 超过 24 小时举报上限；401 / 202 未登录
 */
export function reportPost(data) {
  return request({
    url: '/post/report',
    method: 'post',
    data
  })
}

/**
 * 分享帖子计数（需登录；后端 v0.9.0 #232）。只做外部分享：前端拼链接（utils/share.js 的 postShareUrl）
 * 复制 / 唤起系统分享，同时调本接口。一人一帖只计一次，不可撤销；接口失败不影响分享本身。
 * 文档：后端 docs/api/post-share-api.md
 * @param {string} postId - 帖子 ID
 * @param {string} [expectedSession] - 分享发起时的会话，派发前已切换则取消请求
 * @returns {Promise} data: { share_count, first_share }。失败时 reject：
 *   401 / 202 未登录（访客不要调用）；404 / 204 帖子不可分享；503 / 212 服务端已回滚，可直接重试（utils/share.js 的 isShareRetryableError）
 */
export function sharePost(postId, expectedSession) {
  return request({
    url: '/post/share',
    ...(expectedSession !== undefined && { expectedSession }),
    method: 'post',
    data: { post_id: postId }
  })
}
