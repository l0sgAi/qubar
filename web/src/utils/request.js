import axios from 'axios'
import { sanitizePayload } from '@/utils/sanitize'
import { isFeedTabRestricted } from '@/utils/guest-access'
import { auth } from '@/utils/auth'
import { API_BASE } from '@/config'

// 创建 axios 实例
const request = axios.create({
  baseURL: API_BASE,
  headers: {
    // TODO: 解决拦截问题，生产环境可以去掉
    "ngrok-skip-browser-warning": "true" 
  },
  timeout: 10000
})

// 请求拦截器
request.interceptors.request.use(
  config => {
    // 从 localStorage 获取 token
    const token = auth.getToken()
    // 分享发起时绑定会话，连同 axios 真正发送前的这段异步窗口一起校验。
    if ('expectedSession' in config && config.expectedSession !== token) {
      throw new axios.CanceledError('Session changed')
    }
    if (token) {
      // 设置 sa-token 的请求头
      config.headers['satoken'] = token
    }
    // 清洗请求体中的非法字节（如 NULL 字节 U+0000），
    // 避免后端 PostgreSQL 报 "invalid byte sequence for encoding UTF8: 0x00"。
    // 富文本/Markdown 内容是 NULL 字节的主要来源，故只清洗 data 即可覆盖。
    // 注意：FormData 不走此清洗——其文件字段不可枚举，递归会把整个 FormData 变成空 {}，
    // 导致上传的文件丢失；FormData 也无 NULL 字节文本风险。
    if (config.data && !(config.data instanceof FormData)) {
      config.data = sanitizePayload(config.data)
    }
    return config
  },
  error => {
    console.error('请求错误:', error)
    return Promise.reject(error)
  }
)

/**
 * 判断请求是否属于认证流程（登录/注册）。
 * 这类请求本身不携带有效会话，其返回的 401 含义是“凭证错误”而非“会话过期”，
 * 因此不应触发自动登出 + 重定向，否则会把后端的错误提示连同页面一起刷掉，
 * 应直接 reject 交由调用方（如 LoginCard）展示错误信息。
 */
function isAuthRequest(url = '') {
  return typeof url === 'string' && (
    url.includes('/auth/login') ||
    url.includes('/auth/register') ||
    url.includes('/auth/password')
  )
}

// 在途请求可能属于已退出的账号，只有当前会话的 401 可以触发登出。
function handleUnauthorized(config, message) {
  if ((config?.headers?.satoken ?? null) !== auth.getToken()
      || isAuthRequest(config?.url)
      || isFeedTabRestricted(message)) return
  auth.clearToken()
  window.location.href = '/'
}

// 响应拦截器
request.interceptors.response.use(
  response => {
    const res = response.data

    // 如果 code 不是 200，判断为错误
    if (res.code !== 200) {
      console.error('业务错误:', res.message)

      // 处理特定错误码
      // 登录/注册等认证请求返回的 401 表示“凭证错误”，而非“会话过期”，
      // 此时不应重定向（会刷掉错误提示），应直接交由调用方展示错误。
      // 另：/post/home?tab=recommend|following 访客访问时返回的 401（message='This feed tab requires login'）
      // 仅表示该 tab 需要登录，不应清 token 或跳转，交由调用方降级（如切 hot tab）。
      if (res.code === 401) handleUnauthorized(response.config, res.message)

      // 创建自定义错误对象，保留完整的响应信息
      const error = new Error(res.message || '请求失败')
      error.code = res.code
      error.data = res.data
      return Promise.reject(error)
    }

    return res
  },
  error => {
    // 优先从响应体中获取结构化的错误信息
    if (error.response?.data) {
      const res = error.response.data

      // 处理 401 未授权
      // 登录/注册等认证请求的 401 表示“凭证错误”，应交由调用方展示，不重定向。
      // /post/home 的 feed-tab 限制 401 也不在此处理——交由调用方降级。
      if (error.response.status === 401 || res.code === 401) {
        handleUnauthorized(error.config, res.message)
      }

      // 创建包含后端 message 的错误对象
      // status 为 HTTP 状态码，供调用方区分 503（可重试）/ 404 等；无响应的网络错误不带 status
      const errorObj = new Error(res.message || '请求失败')
      errorObj.code = res.code
      errorObj.data = res.data
      errorObj.status = error.response.status
      return Promise.reject(errorObj)
    }

    // 网络错误或其他无法获取响应的情况
    return Promise.reject(error)
  }
)

export default request
