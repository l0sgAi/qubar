import request from '@/utils/request'
import { auth } from '@/utils/auth'

// 登录类接口统一带上终端类型与终端 ID（多端同时在线，见后端 docs/multi-device-frontend-api.md）
const deviceFields = () => ({ device: 'web', device_id: auth.getDeviceId() })

/**
 * OAuth 登录跳转地址
 * device / device_id 由后端编码进 OAuth state，回跳 /success 时原样带回
 * @param {string} provider - 登录提供商（google / github / azure）
 * @returns {string}
 */
export function loginWithOAuth(provider) {
  const query = new URLSearchParams(deviceFields())
  return `${request.defaults.baseURL}/auth/${provider}/login?${query}`
}

/**
 * 获取当前用户信息
 * @returns {Promise}
 */
export function getUserInfo() {
  return request({
    url: '/user/get',
    method: 'get'
  })
}

/**
 * 退出登录
 * @returns {Promise}
 */
export function logout() {
  return request({
    url: '/auth/logout',
    method: 'post'
  })
}

/**
 * 发送邮箱验证码
 * @param {string} email
 * @returns {Promise}
 */
export function sendVerificationCode(email, lang) {
  return request({
    url: '/auth/register/send-code',
    method: 'post',
    data: { email, ...(lang && { lang }) }
  })
}

/**
 * 验证邮箱验证码
 * @param {string} email
 * @param {string} code - 6位验证码
 * @returns {Promise}
 */
export function verifyEmailCode(email, code) {
  return request({
    url: '/auth/register/verify',
    method: 'post',
    data: { email, code }
  })
}

/**
 * 邮箱密码注册
 * @param {Object} data - { email, username, password, code }
 * @returns {Promise}
 */
export function registerWithEmail(data) {
  const { email, username, password } = data
  return request({
    url: '/auth/register/complete',
    method: 'post',
    data: { email, username, password, ...deviceFields() }
  })
}

/**
 * 邮箱密码登录
 * @param {Object} data - { email, password }
 * @returns {Promise}
 */
export function loginWithEmail(data) {
  const { email, password } = data
  return request({
    url: '/auth/login',
    method: 'post',
    data: { email, password, ...deviceFields() }
  })
}

/**
 * 发送找回密码验证码
 * 与注册流程镜像：邮箱必须已注册（未注册返回 404）
 * @param {string} email
 * @param {string} [lang] - 'zh' | 'en'，邮件语言
 * @returns {Promise}
 */
export function sendPasswordResetCode(email, lang) {
  return request({
    url: '/auth/password/send-code',
    method: 'post',
    data: { email, ...(lang && { lang }) }
  })
}

/**
 * 校验找回密码验证码
 * 校验成功后后端写入 10min 有效的「已验证」标记
 * @param {string} email
 * @param {string} code - 6位验证码
 * @returns {Promise}
 */
export function verifyPasswordResetCode(email, code) {
  return request({
    url: '/auth/password/verify',
    method: 'post',
    data: { email, code }
  })
}

/**
 * 重置密码
 * 注意：字段名是 new_password（snake_case），不是 password
 * 成功后不会返回 token，且会踢下线该账号所有设备，需引导用户重新登录
 * @param {string} email
 * @param {string} newPassword - 至少 6 位
 * @returns {Promise}
 */
export function resetPassword(email, newPassword) {
  return request({
    url: '/auth/password/reset',
    method: 'post',
    data: { email, new_password: newPassword }
  })
}
