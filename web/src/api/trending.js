import request from '@/utils/request'

/**
 * 热点榜单（跨域编排器，聚合 帖子/圈子/用户 三类热度榜）
 * @param {Object} params - 查询参数
 * @param {string} params.window - 时间窗 '24h' | '7d'，默认 24h，非法回落 24h
 * @param {string} params.section - 板块 'all' | 'posts' | 'circles' | 'users'，默认 all（三类同返）
 * @param {number} params.size - 每板块条数 1~50，默认 20，超出回落 20
 * @param {number} params.offset - 单板块翻页偏移（section=all 时忽略）
 * @returns {Promise} data 为 TrendingBoard：{ window, posts?, circles?, users?, refreshed_at, truncated, offset, size }
 */
// 注：后端路由注册为 /trending/（带尾斜杠），无斜杠访问会被框架 301 重定向且该 301
// 不带 CORS 头，浏览器跨域直接拦截。这里用带斜杠的 URL 绕过 301。
// 治本应改后端：路由注册为 /trending，或让 CORS 中间件覆盖 301。
export function getTrending(params) {
  return request({
    url: '/trending/',
    method: 'get',
    params
  })
}
