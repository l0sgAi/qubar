/**
 * 全局「访客操作拦截 → 登录引导」总线。
 *
 * 背景：点赞/收藏/评论/加圈等写操作需登录。访客点击这些操作时，
 * 不应让请求直接打到后端触发 401 硬跳转（会刷新页面、丢失上下文），
 * 而应前置拦截，弹一个轻量登录引导 Modal。
 *
 * 设计：用 Vue 的 ref 作为单例状态，App.vue 挂一个全局 <LoginPromptModal> 监听它。
 * 各业务组件只需 `import { requireLogin } from '@/utils/guest-action'`，
 * 在写操作 handler 顶部调用 `if (!auth.isAuthenticated()) { requireLogin('like'); return }`。
 *
 * action 参数（可选）：用于在引导文案里说明「登录后即可 XX」，
 * 取值见 i18n key login.guestPrompt.{like|collect|comment|join}。
 */
import { ref } from 'vue'

// 全局状态：当前是否展示引导 Modal，以及触发它的 action（用于文案）
const showPrompt = ref(false)
const promptAction = ref('') // 'like' | 'collect' | 'comment' | 'join' | ''

/**
 * 触发登录引导。
 * @param {string} [action] 触发的操作类型，用于引导文案（见 login.guestPrompt）
 */
export function requireLogin(action = '') {
  promptAction.value = action
  showPrompt.value = true
}

/**
 * 关闭登录引导（供 Modal 内部调用）。
 */
export function closeLoginPrompt() {
  showPrompt.value = false
  promptAction.value = ''
}

export function useLoginPrompt() {
  return {
    showPrompt,
    promptAction
  }
}
