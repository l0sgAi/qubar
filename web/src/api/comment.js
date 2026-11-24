import request from '@/utils/request'

// 业务码：评论不存在 / 已删除 / 因审核不可见（对接文档 §3.2，前后端共享约定）
export const COMMENT_NOT_FOUND_CODE = 40401

/**
 * 评论 / 所属帖子不可见：业务码 40401，或信封 204 / HTTP 404。
 * 私密圈非成员访问评论 list / replies / locate / detail / create 时返回与「不存在」相同的 404（后端 #170）。
 */
export function isCommentUnavailableError(error) {
  return error?.code === COMMENT_NOT_FOUND_CODE || error?.code === 204 || error?.status === 404
}

/**
 * 发表评论
 * @param {Object} data - 评论数据
 * @param {string} data.post_id - 帖子ID(UUIDv7)
 * @param {string} data.content - 评论内容
 * @returns {Promise}
 */
export function createComment(data) {
  return request({
    url: '/comment/create',
    method: 'post',
    data
  })
}

/**
 * 定位评论（通知点击直达评论用）
 * @param {Object} params
 * @param {string} params.comment_id - 目标评论ID(UUIDv7)，顶层评论或回复均可
 * @param {number} [params.sort] - 顶层列表排序：0=按点赞倒序（默认），1=按时间倒序；返回的 list_cursor 仅在同 sort 下有效
 * @param {number} [params.reply_sort] - 回复列表排序：0=最热，1=最新（默认，对齐回复列表用法）
 * @returns {Promise} data: { comment_id, post_id, root_id, is_root, list_cursor, reply_cursor, reply_page }
 *   目标不存在/已删除/不可见时 reject，error.code === COMMENT_NOT_FOUND_CODE
 */
export function locateComment(params) {
  return request({
    url: '/comment/locate',
    method: 'get',
    params
  })
}

/**
 * 获取顶层评论列表
 * @param {Object} params
 * @param {string} params.post_id - 帖子ID(UUIDv7)
 * @param {number} [params.sort] - 0=按点赞倒序（默认），1=按时间倒序
 * @param {string} [params.cursor] - 游标，首页不传
 * @returns {Promise}
 */
export function getCommentList(params) {
  return request({
    url: '/comment/list',
    method: 'get',
    params
  })
}

/**
 * 加载子回复列表（页码分页）
 * @param {Object} params
 * @param {string} params.root_id - 根评论ID(UUIDv7)
 * @param {number} [params.sort] - 0=最热（点赞倒序），1=最新（时间倒序，默认）
 * @param {number} [params.page] - 页码，从1开始
 * @param {number} [params.page_size] - 每页数量，默认5
 * @returns {Promise}
 */
export function getCommentReplies(params) {
  return request({
    url: '/comment/replies',
    method: 'get',
    params
  })
}
