import { describe, it, expect } from 'vitest'
import {
  API_CODE,
  isBadRequestError,
  isUnauthorizedError,
  isForbiddenError,
  isNotFoundError,
  isConflictError,
  isRateLimitedError,
  isServiceUnavailableError,
  isServerError,
  isNetworkError,
  isRetryableError,
  errorMessageIncludes
} from '@/utils/apiError'

// 构造 request 拦截器 reject 的错误形态
const apiErr = (code, status, message = '') => Object.assign(new Error(message), { code, status })

describe('utils/apiError', () => {
  it.each([
    [isBadRequestError, API_CODE.BAD_REQUEST, 400],
    [isUnauthorizedError, API_CODE.UNAUTHORIZED, 401],
    [isForbiddenError, API_CODE.FORBIDDEN, 403],
    [isNotFoundError, API_CODE.NOT_FOUND, 404],
    [isConflictError, API_CODE.CONFLICT, 409],
    [isRateLimitedError, API_CODE.TOO_MANY_REQUESTS, 429],
    [isServiceUnavailableError, API_CODE.SERVICE_UNAVAILABLE, 503]
  ])('%o：信封 code 与 HTTP 状态任一命中即可', (fn, code, status) => {
    expect(fn(apiErr(code, status))).toBe(true)
    expect(fn(apiErr(code, undefined))).toBe(true) // HTTP 200 + 业务错误
    expect(fn(apiErr(undefined, status))).toBe(true)
    expect(fn(apiErr(999, 418))).toBe(false)
    expect(fn(null)).toBe(false)
  })

  it('isServerError：5xx 或信封 500 / 210 / 212', () => {
    expect(isServerError(apiErr(500, 500))).toBe(true)
    expect(isServerError(apiErr(undefined, 502))).toBe(true)
    expect(isServerError(apiErr(500))).toBe(true)
    expect(isServerError(apiErr(210))).toBe(true)
    expect(isServerError(apiErr(212))).toBe(true)
    expect(isServerError(apiErr(208, 429))).toBe(false)
    expect(isServerError(undefined)).toBe(false)
  })

  it('isNetworkError：没有 status 也没有数字 code', () => {
    expect(isNetworkError(Object.assign(new Error('timeout'), { code: 'ECONNABORTED' }))).toBe(true)
    expect(isNetworkError(new Error('Network Error'))).toBe(true)
    expect(isNetworkError(apiErr(201))).toBe(false)
    expect(isNetworkError(apiErr(undefined, 500))).toBe(false)
    expect(isNetworkError(null)).toBe(false)
  })

  it('isRetryableError：网络错误或服务端错误', () => {
    expect(isRetryableError(new Error('Network Error'))).toBe(true)
    expect(isRetryableError(apiErr(500, 500))).toBe(true)
    expect(isRetryableError(apiErr(208, 429))).toBe(false)
    expect(isRetryableError(apiErr(203, 403))).toBe(false)
  })

  it('errorMessageIncludes：不区分大小写，空片段 / 无 message 为 false', () => {
    const err = apiErr(201, 400, 'Cannot follow yourself')
    expect(errorMessageIncludes(err, 'cannot FOLLOW')).toBe(true)
    expect(errorMessageIncludes(err, '')).toBe(false)
    expect(errorMessageIncludes({ message: 42 }, '4')).toBe(false)
    expect(errorMessageIncludes(null, 'x')).toBe(false)
  })
})
