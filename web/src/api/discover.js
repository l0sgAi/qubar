import request from '@/utils/request'

/**
 * 发现页（跨域编排器，随机推送 圈子/帖子 两个分区）
 * 与首页推荐 / 热点榜不同：发现页是「发散」——随机推送你没看过的内容。
 *
 * - 鉴权「可选登录」：带 satoken → 反气泡个性化（排除已加入圈子/已交互帖）；
 *   不带或失效 token → 纯随机（匿名落地页），不返回 401。
 * - 内容来自预计算的随机候选池（后台约 10 分钟重建一次），同一周期内翻页稳定；
 *   池重建时翻页会返回 pool_refreshed:true + offset:0 + 新 pool_token。
 *
 * @param {Object} params - 查询参数
 * @param {string} params.section - 'all' | 'posts' | 'circles'，默认 all（两分区同返）
 * @param {number} params.size - 每分区条数 1~50，默认 20，超出回落 20
 * @param {number} params.offset - 单分区翻页偏移（section=all 时忽略）
 * @param {string} params.pool_token - 上一页响应返回的池版本 token，翻页原样回传
 * @returns {Promise} data 为 DiscoverBoard：
 *   { circles?, posts?, pool_token, has_more, pool_refreshed, offset, size }
 */
export function getDiscover(params) {
  return request({
    // 注：后端路由注册为 /discover/（带尾斜杠），无斜杠访问会被框架 301 重定向且该 301
    // 不带 CORS 头，浏览器跨域直接拦截（表现：Network Error + 后端无日志）。
    // 这里用带斜杠的 URL 绕过 301，与 trending.js 同样的处理。
    // 治本应改后端：路由注册为 /discover，或让 CORS 中间件覆盖 301。
    url: '/discover/',
    method: 'get',
    params
  })
}
