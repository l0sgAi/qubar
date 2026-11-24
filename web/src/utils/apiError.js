/**
 * 接口错误分类（配合 utils/request.js 的拦截器）。
 *
 * request 拦截器 reject 的错误对象：
 * - error.code：后端信封 code（201 参数错误 / 202 未登录 / 203 无权限 / 204 不存在 / 207 冲突 /
 *   208 频率限制 / 210、500 服务器错误 / 212 服务暂不可用）
 * - error.status：HTTP 状态码（HTTP 200 + 业务错误时没有）
 * - error.message：后端英文 message（不直接展示，只用来区分同一 code 下的场景）
 * - 网络错误 / 超时：axios 原始错误，没有 code（或为 'ECONNABORTED' 等字符串）也没有 status
 *
 * 信封 code 与 HTTP 状态一一对应，两者都认。
 */

export const API_CODE = Object.freeze({
  BAD_REQUEST: 201,
  UNAUTHORIZED: 202,
  FORBIDDEN: 203,
  NOT_FOUND: 204,
  CONFLICT: 207,
  TOO_MANY_REQUESTS: 208,
  INTERNAL: 210,
  SERVICE_UNAVAILABLE: 212
})

const matches = (error, code, status) =>
  !!error && (error.code === code || error.status === status)

export const isBadRequestError = (error) => matches(error, API_CODE.BAD_REQUEST, 400)
export const isUnauthorizedError = (error) => matches(error, API_CODE.UNAUTHORIZED, 401)
export const isForbiddenError = (error) => matches(error, API_CODE.FORBIDDEN, 403)
export const isNotFoundError = (error) => matches(error, API_CODE.NOT_FOUND, 404)
export const isConflictError = (error) => matches(error, API_CODE.CONFLICT, 409)
export const isRateLimitedError = (error) => matches(error, API_CODE.TOO_MANY_REQUESTS, 429)
export const isServiceUnavailableError = (error) => matches(error, API_CODE.SERVICE_UNAVAILABLE, 503)

/** 5xx / 信封 500、210、212 */
export function isServerError(error) {
  if (!error) return false
  if (typeof error.status === 'number' && error.status >= 500) return true
  return error.code === 500 || error.code === API_CODE.INTERNAL || error.code === API_CODE.SERVICE_UNAVAILABLE
}

/** 没拿到后端响应：网络断开、超时、被浏览器拦截（axios 原始错误，没有 status 也没有数字 code） */
export function isNetworkError(error) {
  if (!error) return false
  return error.status === undefined && typeof error.code !== 'number'
}

/** 请求是否可以原样重试（网络错误 / 超时 / 5xx）。幂等性由调用方保证（如私信的 client_msg_id） */
export function isRetryableError(error) {
  return isNetworkError(error) || isServerError(error)
}

/** 后端英文 message 是否包含片段（不区分大小写），用于同一 code 下区分场景 */
export function errorMessageIncludes(error, fragment) {
  const msg = typeof error?.message === 'string' ? error.message.toLowerCase() : ''
  return !!fragment && msg.includes(String(fragment).toLowerCase())
}
