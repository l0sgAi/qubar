import request from '@/utils/request'

// 用户关注（后端 v0.9.0，l0sgAi/qubar#217 / #218）。对接文档：后端仓 docs/api/follow-api.md。
// relation（当前登录用户与目标用户的关系）：0 未关注 / 1 我关注了对方 / 2 对方关注了我 / 3 互关；
// 访客或看自己恒为 0。常量与错误判断见 utils/follow.js。

/** 批量关系查询单次上限（后端超过返回 400） */
export const FOLLOW_RELATIONS_BATCH = 100

/**
 * 关注（需登录，幂等）
 * @param {string} userId - 目标用户 ID
 * @returns {Promise} data: { relation }（1 或 3）。上限 400 / 自己 400 / 用户不存在 404
 */
export function followUser(userId) {
  return request({
    url: `/follow/${userId}`,
    method: 'post'
  })
}

/**
 * 取关（需登录，幂等；对方已禁用 / 删除也可以取关）
 * @param {string} userId - 目标用户 ID
 * @returns {Promise} data: { relation }（0 或 2）
 */
export function unfollowUser(userId) {
  return request({
    url: `/follow/${userId}`,
    method: 'delete'
  })
}

/**
 * 关注数 / 粉丝数 / 关系 / 列表是否可见（访客可读）
 * @param {string} userId - 目标用户 ID
 * @returns {Promise} data: { user_id, following_count, follower_count, relation, list_visible }。用户不存在 404
 */
export function getFollowStats(userId) {
  return request({
    url: `/follow/stats/${userId}`,
    method: 'get'
  })
}

/**
 * 关注列表（访客可读，按关注时间倒序）。对方设为私密且不是本人时 403。
 * 列表结束只看 has_more：服务端会过滤已禁用 / 删除的用户，一页可能不足 size 条甚至为空。
 * @param {string} userId - 列表主人 ID
 * @param {Object} [params]
 * @param {string} [params.cursor] - 上一页的 next_cursor，原样回传；不传为第一页（不能与粉丝列表游标混用）
 * @param {number} [params.size] - 默认 20，最大 50
 * @returns {Promise} data: { items: [{ user: {id, username, avatar_url}, follow_time, relation }], has_more, next_cursor }
 *   （items[].relation 是「我」与该项用户的关系）
 */
export function getFollowingList(userId, params) {
  return request({
    url: `/follow/following/${userId}`,
    method: 'get',
    params
  })
}

/**
 * 粉丝列表（访客可读）。参数、响应、错误同 getFollowingList。
 * @param {string} userId - 列表主人 ID
 * @param {Object} [params] - { cursor, size }
 */
export function getFollowerList(userId, params) {
  return request({
    url: `/follow/followers/${userId}`,
    method: 'get',
    params
  })
}

/**
 * 我的互关列表（需登录，只能看自己的，不受隐私开关影响）。items[].relation 恒为 3。
 * @param {Object} [params] - { cursor, size }
 * @returns {Promise} 结构同 getFollowingList
 */
export function getMutualList(params) {
  return request({
    url: '/follow/mutual',
    method: 'get',
    params
  })
}

/**
 * 批量查询我与一组用户的关系（需登录）。
 * 空数组直接返回空结果不发请求；超过 100 个自动分批并合并结果。
 * @param {string[]} userIds
 * @returns {Promise<{ code: number, data: { relations: Record<string, number> } }>}
 */
export async function getFollowRelations(userIds) {
  const ids = [...new Set((userIds || []).filter(Boolean))]
  if (ids.length === 0) return { code: 200, data: { relations: {} } }

  const chunks = []
  for (let i = 0; i < ids.length; i += FOLLOW_RELATIONS_BATCH) {
    chunks.push(ids.slice(i, i + FOLLOW_RELATIONS_BATCH))
  }
  const results = await Promise.all(chunks.map(chunk => request({
    url: '/follow/relations',
    method: 'post',
    data: { user_ids: chunk }
  })))
  const relations = Object.assign({}, ...results.map(res => res?.data?.relations || {}))
  return { code: 200, data: { relations } }
}
