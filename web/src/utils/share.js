// 分享 / 复制链接（移动端门户用，见 docs/mobile-adaptation-plan.md 2.2 / 6.3）
// 帖子分享计数（后端 v0.9.0 POST /post/share，api/post.js 的 sharePost）的链接与错误判断也在这里。
import { isNotFoundError, isServiceUnavailableError } from '@/utils/apiError'

/**
 * 复制文本到剪贴板。优先异步 Clipboard API（需安全上下文），失败回落 execCommand。
 * @returns {Promise<boolean>} 是否复制成功
 */
export async function copyText(text) {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 权限被拒 / 非安全上下文：走降级
  }
  let el = null
  try {
    el = document.createElement('textarea')
    el.value = text
    el.setAttribute('readonly', '')
    el.style.position = 'fixed'
    el.style.opacity = '0'
    document.body.appendChild(el)
    el.select()
    return document.execCommand('copy')
  } catch {
    return false
  } finally {
    el?.remove()
  }
}

/**
 * 调起系统分享面板；不支持时复制链接。
 * @returns {Promise<'shared'|'cancelled'|'copied'|'failed'>}
 */
export async function shareOrCopy({ title, url }) {
  if (navigator.share) {
    try {
      await navigator.share({ title, url })
      return 'shared'
    } catch (err) {
      // 用户关闭分享面板：不再回落复制，避免打扰
      if (err?.name === 'AbortError') return 'cancelled'
    }
  }
  return (await copyText(url)) ? 'copied' : 'failed'
}

/** 帖子落地页路径（访客可读的帖子详情；链接里不带用户 ID / token） */
export function postSharePath(postId) {
  return `/post/${encodeURIComponent(postId)}`
}

/** 帖子分享链接：当前站点 origin + 帖子详情路径 */
export function postShareUrl(postId, origin = window.location.origin) {
  return `${origin}${postSharePath(postId)}`
}

/** POST /post/share 返回 503（code 212）：服务端已回滚、本次未计数，可直接重试（只会计一次） */
export function isShareRetryableError(error) {
  return isServiceUnavailableError(error)
}

/**
 * POST /post/share 失败 → i18n key；返回 null 表示静默忽略（不影响已完成的复制 / 系统分享）。
 * - 404：帖子不存在 / 未发布 / 已删除 / 私密圈非成员 → 提示内容不可分享
 * - 503：可重试 → 提示计数失败可重试
 * - 401 / 其它：静默（访客本就不应调用；其它服务端错误不打扰用户）
 */
export function shareErrorKey(error) {
  if (isNotFoundError(error)) return 'share.error.notFound'
  if (isShareRetryableError(error)) return 'share.error.retry'
  return null
}
