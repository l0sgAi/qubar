import request from '@/utils/request'

/**
 * 获取通知列表（keyset 游标分页，最新在前）
 * @param {Object} params
 * @param {number|string} [params.type] - 空/0=全部（默认）；单值 1-8 或逗号分隔多值（如 '1,2'=赞、'7,8'=审核结果）按类型集合过滤。
 *   1 赞帖 / 2 赞评论 / 3 收藏 / 4 评论 / 5 回复 / 6 提及 / 7 帖子审核通过 / 8 帖子审核驳回（7/8 不带 actor 与 comment_id）。
 *   9–11 需后端 l0sgAi/qubar#154 上线后才能传，旧后端收到会返回 400
 * @param {number} [params.size] - 每页条数，默认20，>100 回落20
 * @param {string} [params.cursor] - 上一页返回的游标，首页不传；响应 cursor 为空字符串=没有更多
 * @returns {Promise} data: { notices, size, cursor }
 */
export function getNoticeList(params) {
  return request({
    url: '/notice/list',
    method: 'get',
    params
  })
}

/**
 * 获取未读通知数（红点/角标）
 * @returns {Promise} data: { unread_count }
 */
export function getUnreadCount() {
  return request({
    url: '/notice/unread-count',
    method: 'get'
  })
}

/**
 * 批量标记已读（幂等，可安全重试；仅标记当前用户自己的通知）
 * @param {string[]} ids - 通知ID列表（uuid，1-100 个）
 * @returns {Promise} data 为 null，以 code 判成功
 */
export function markNoticesRead(ids) {
  return request({
    url: '/notice/read',
    method: 'post',
    data: { ids }
  })
}

/**
 * 全部标记已读（未读数归零）
 * @returns {Promise} data 为 null，以 code 判成功
 */
export function markAllNoticesRead() {
  return request({
    url: '/notice/read-all',
    method: 'post'
  })
}
