import request from '@/utils/request'
import { MODERATION_PAGE_SIZE, parseModerationSummary } from '@/constants/moderation'

// ==================== 圈内内容审核（/circle/moderation）====================
// 权限：本圈圈主（30）/ 管理员（20）且成员状态正常；其余一律 403（code=203），不区分原因。
// 错误码按信封 code 区分：201=参数/游标非法（400）、203=无权限（403）、204=帖子不存在或已不在审核中（404）。

/**
 * 获取本圈待审帖子列表（status=2 且未删除，按发帖时间倒序）
 * @param {Object} params - 查询参数
 * @param {string} params.circle_id - 圈子 ID（必填）
 * @param {number} [params.size] - 每页条数，≤0 或 >100 按 20 处理
 * @param {string} [params.cursor] - 上一页返回的 cursor 原样回传；首页不传。非法游标返回 201
 * @returns {Promise} data: { posts: ModerationPost[], size: number, cursor: string }
 *   cursor 为空字符串 = 没有更多；
 *   posts[]：id / circle_id / user_id / type / title / summary / content（全文）/ media_extra（图片 URL 数组）/
 *   status / create_time / author_name / author_avatar / ai_verdict。
 *   ai_verdict 可能为 null（AI 尚未处理）；字段 decision / hit_hazards（按概率降序）/ hazards（key→0–1）/
 *   severity（0–3，AI 不可用时 null）/ severity_confidence（0–1）/ model / error_msg（非空=AI 不可用）/ create_time
 */
export function getModerationPosts(params) {
  return request({
    url: '/circle/moderation/posts',
    method: 'get',
    params
  })
}

/**
 * 人工审核待审帖子：通过（→ status=1）或驳回（→ status=3），两种结果都会通知作者。
 * 并发处理同一帖子只有一次生效，其余返回 204（前端提示「已被处理」并刷新列表）。
 * approve 后进入搜索 / 圈子列表依赖 CDC 同步，有数秒延迟。
 * @param {Object} data - 审核数据
 * @param {string} data.circle_id - 帖子所在圈子 ID（须与帖子实际所属圈子一致）
 * @param {string} data.post_id - 待审帖子 ID
 * @param {'approve'|'reject'} data.action - 必须显式传，其他值（含空）返回 201
 * @param {string} [data.reason] - 备注，≤200 字符（超出截断）；只记入审核日志，不展示给作者
 * @returns {Promise} data: { post_id: string, status: number }
 */
export function reviewModerationPost(data) {
  return request({
    url: '/circle/moderation/review',
    method: 'post',
    data
  })
}

// ==================== 举报复核（#153）====================
// 404（code=204）：帖子不存在 / 不在该圈 / 没有待处理举报（含已被他人处理）

/**
 * 获取本圈被举报帖子列表：有待处理举报、且帖子已发布（1）或已被自动屏蔽（4），按本轮首次举报时间倒序
 * @param {Object} params - 查询参数，同 getModerationPosts（circle_id / size / cursor）
 * @returns {Promise} data: { posts: ReportedPost[], size: number, cursor: string }
 *   cursor 为空字符串 = 没有更多；posts[] 在待审列表字段基础上另带：
 *   report_count（本轮待处理举报总数）/ first_report_time（本轮首次举报时间）/
 *   reports（最近 10 条待处理举报，新→旧，每条 { reason_type: 1–8, reason, create_time }，不含举报人）。
 *   status 为 4 时帖子已被自动屏蔽（仅作者可见）；ai_verdict 可能为 null（关闭审核或审核上线前发的帖子）
 */
export function getModerationReports(params) {
  return request({
    url: '/circle/moderation/reports',
    method: 'get',
    params
  })
}

/**
 * 处理被举报帖子：保留（dismiss，关闭本轮全部举报，被屏蔽的帖子恢复为 1）或下架（remove，→ status=3，通知作者）。
 * 并发处理同一帖子只有一次生效，其余返回 204。
 * @param {Object} data - 处理数据
 * @param {string} data.circle_id - 帖子所在圈子 ID
 * @param {string} data.post_id - 被举报帖子 ID
 * @param {'dismiss'|'remove'} data.action - 其他值返回 201
 * @param {string} [data.reason] - 备注，≤200 字符；只记入审核日志，不展示给作者
 * @returns {Promise} data: { post_id: string, status: number, closed_reports: number }
 */
export function resolveModerationReport(data) {
  return request({
    url: '/circle/moderation/reports/resolve',
    method: 'post',
    data
  })
}

/**
 * 审核待处理数（l0sgAi/qubar#156，未上线时接口不存在 → reject，调用方回落到「拉列表首页」估算）
 * @param {Object} params
 * @param {string} params.circle_id - 圈子 ID
 * @returns {Promise} data: { review_pending: number, report_pending: number }
 */
export function getModerationSummary(params) {
  return request({
    url: '/circle/moderation/summary',
    method: 'get',
    params
  })
}

/**
 * 两个审核队列的待处理数（管理入口 / 页头角标用）：优先 summary 接口的准确数字；
 * 接口未上线 / 失败时回落为各拉一页（size=20）计数，任一满页 full=true（显示「20+」）。
 * 回落时两个列表请求互不影响，单个失败按 0 计
 * @param {string} circleId - 圈子 ID
 * @returns {Promise<{ reported: number, pending: number, full: boolean }>}
 */
export async function getModerationCounts(circleId) {
  try {
    const summary = parseModerationSummary((await getModerationSummary({ circle_id: circleId })).data)
    if (summary) return { ...summary, full: false }
  } catch {
    // 回落到列表计数
  }
  const params = { circle_id: circleId, size: MODERATION_PAGE_SIZE }
  const [reports, posts] = await Promise.allSettled([getModerationReports(params), getModerationPosts(params)])
  const count = (r) => (r.status === 'fulfilled' ? r.value.data?.posts?.length || 0 : 0)
  const reported = count(reports)
  const pending = count(posts)
  return { reported, pending, full: reported >= MODERATION_PAGE_SIZE || pending >= MODERATION_PAGE_SIZE }
}
