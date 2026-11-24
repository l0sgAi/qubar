import { ref } from 'vue'
import { createComment, isCommentUnavailableError } from '@/api/comment'
import { getUserInfo } from '@/api/auth'
import { seedContentMentions } from '@/utils/mentionResolve'

/**
 * 评论 / 回复提交（CommentEditor、CommentReplyEditor、移动端 PlainCommentInput 共用）。
 * 只负责：调接口 → 补作者信息 → 组装乐观渲染对象 → 回灌 mentions。
 * 成功 / 失败提示、清空输入、emit 由调用方按各自原有顺序处理；失败时抛出原始错误。
 *
 * @returns {{ submitting: Ref<boolean>, submit: Function }}
 */
/**
 * 评论 / 回复提交失败的提示文案。帖子已不可见（删除 / 私密圈非成员，后端 404）时给自有文案，
 * 不展示后端英文 message；其它错误沿用原逻辑（后端 message 优先，缺省时用 fallbackKey）。
 */
export function commentSubmitErrorMessage(err, t, fallbackKey) {
  if (isCommentUnavailableError(err)) return t('comment.unavailable')
  return err?.message || t(fallbackKey)
}

export function useCommentSubmit() {
  const submitting = ref(false)

  /**
   * @param {Object} params
   * @param {string} params.postId
   * @param {string} params.content
   * @param {Object} [params.reply] - 回复时传入：{ rootId, replyToId, replyToName }；
   *   replyToId 为 null 表示直接回复根评论（root_id 取 rootId）
   * @param {Object|null} [params.extraData] - 如 { images: [...] }
   * @param {string[]} [params.mentionIds] - 被 @ 用户 uuid（空数组不传给后端）
   * @returns {Promise<Object>} 乐观渲染用的评论对象
   */
  const submit = async ({ postId, content, reply = null, extraData = null, mentionIds = [] }) => {
    submitting.value = true
    try {
      const mentionUserIds = mentionIds.length ? mentionIds : undefined
      const res = await createComment(reply
        ? {
            post_id: postId,
            root_id: reply.rootId ?? reply.replyToId,
            reply_to_id: reply.replyToId,
            content,
            extra_data: extraData,
            mention_user_ids: mentionUserIds
          }
        : {
            post_id: postId,
            content,
            extra_data: extraData,
            mention_user_ids: mentionUserIds
          })

      let userData = {}
      try {
        const userRes = await getUserInfo()
        if (userRes.data) {
          userData = userRes.data
        }
      } catch (err) {
        console.error('获取用户信息失败:', err)
      }

      const created = typeof res.data === 'object' && res.data !== null
        ? { ...res.data }
        : { id: res.data }

      created.author_name = userData.name || created.author_name || ''
      created.author_id = userData.id || created.author_id
      created.author_avatar = userData.avatar_url || created.author_avatar || null
      created.content = created.content || content
      created.like_count = created.like_count || 0
      if (reply) {
        created.reply_to_name = created.reply_to_name || reply.replyToName || null
      } else {
        created.reply_count = created.reply_count || 0
      }
      created.create_time = created.create_time || new Date().toISOString()
      if (extraData) {
        created.extra_data = extraData
      }

      // 后端回传 mentions（契约见 docs/mention-user-ids-integration.md），乐观渲染前回灌
      seedContentMentions([created])
      return created
    } finally {
      submitting.value = false
    }
  }

  return { submitting, submit }
}
