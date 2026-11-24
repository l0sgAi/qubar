import { customRef } from 'vue'

const TOKEN_KEY = 'qubar_token'
const TOKEN_EXPIRE_KEY = 'qubar_token_expire'
// 终端实例 ID（多端同时在线，对接文档见后端 docs/multi-device-frontend-api.md）：
// 同一浏览器所有标签页共用，登出 / 换账号 / 重置密码都不清除，只有清站点数据才会变
const DEVICE_ID_KEY = 'qubar_device_id'

// 后端只认标准 UUID（8-4-4-4-12），全零 UUID 也按没传处理
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
const NIL_UUID = '00000000-0000-0000-0000-000000000000'
const isValidDeviceId = (id) => typeof id === 'string' && UUID_RE.test(id) && id !== NIL_UUID

// crypto.randomUUID 只在安全上下文（https / localhost）可用；
// 用局域网 IP 调试移动端时回退到 getRandomValues 拼一个 v4 UUID
const randomUUID = () => {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const b = crypto.getRandomValues(new Uint8Array(16))
  b[6] = (b[6] & 0x0f) | 0x40
  b[8] = (b[8] & 0x3f) | 0x80
  const h = Array.from(b, x => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

// OAuth 整页跳转前暂存的站内回跳地址（sessionStorage），由登录成功回调页消费后移除
export const OAUTH_RETURN_PATH_KEY = 'qubar_oauth_return_path'

// 旧版键名（quba_*）一次性迁移，避免已登录用户掉登录状态
const legacyToken = localStorage.getItem('quba_token')
if (legacyToken && !localStorage.getItem(TOKEN_KEY)) {
  localStorage.setItem(TOKEN_KEY, legacyToken)
  const legacyExpire = localStorage.getItem('quba_token_expire')
  if (legacyExpire) {
    localStorage.setItem(TOKEN_EXPIRE_KEY, legacyExpire)
  }
}
localStorage.removeItem('quba_token')
localStorage.removeItem('quba_token_expire')

// 保留 localStorage 为会话来源，同时让已挂载组件观察本页与其它标签页的账号切换。
const sessionToken = customRef((track, trigger) => {
  window.addEventListener('storage', (event) => {
    if (event.key === TOKEN_KEY || event.key === TOKEN_EXPIRE_KEY || event.key === null) trigger()
  })
  return {
    get() {
      track()
      return localStorage.getItem(TOKEN_KEY)
    },
    set(token) {
      if (token === null) localStorage.removeItem(TOKEN_KEY)
      else localStorage.setItem(TOKEN_KEY, token)
      trigger()
    }
  }
})

export const auth = {
  /**
   * 保存 token
   * @param {string} token - sa-token
   * @param {number} expire - 过期时间（秒）
   */
  setToken(token, expire) {
    if (expire) {
      // 计算过期时间戳（毫秒）
      const expireTime = Date.now() + parseInt(expire) * 1000
      localStorage.setItem(TOKEN_EXPIRE_KEY, expireTime.toString())
    }
    sessionToken.value = token
  },

  /**
   * 获取 token
   * @returns {string|null}
   */
  getToken() {
    return sessionToken.value
  },

  /**
   * 检查 token 是否有效
   * @returns {boolean}
   */
  isTokenValid() {
    const token = this.getToken()
    if (!token) {
      return false
    }

    const expireTime = localStorage.getItem(TOKEN_EXPIRE_KEY)
    if (expireTime) {
      // 检查是否过期
      const now = Date.now()
      return now < parseInt(expireTime)
    }

    return true
  },

  /**
   * 清除 token
   */
  clearToken() {
    // 不清 device_id：下次登录仍是同一终端，只替换本终端的旧 token
    localStorage.removeItem(TOKEN_EXPIRE_KEY)
    sessionToken.value = null
  },

  /**
   * 获取终端 ID，本地没有（或不合法）时生成并保存
   * @param {string} [fallback] - 本地没有时优先采用的值（如 OAuth 回跳 URL 带回的），不合法则忽略
   * @returns {string}
   */
  getDeviceId(fallback) {
    let id = localStorage.getItem(DEVICE_ID_KEY)
    if (!isValidDeviceId(id)) {
      id = isValidDeviceId(fallback) ? fallback.toLowerCase() : randomUUID()
      localStorage.setItem(DEVICE_ID_KEY, id)
    }
    return id
  },

  /**
   * 保存登录响应里的终端 ID（以服务端为准；没返回或不合法时保持本地值）
   * @param {string} [id]
   */
  saveDeviceId(id) {
    if (isValidDeviceId(id)) {
      localStorage.setItem(DEVICE_ID_KEY, id.toLowerCase())
    }
  },

  /**
   * 检查是否已登录
   * @returns {boolean}
   */
  isAuthenticated() {
    return this.isTokenValid()
  }
}
