// @提及 已选用户状态：MentionPicker（按钮版）与 MentionTrigger（编辑器内联版）共用一份
// mentionedUsers；提交时由 filterMentionedIds(content, mentionedUsers) 过滤出仍存在于正文的
// uuid 传给后端。去重 + 10 人上限在此统一拦截，两处入口共享同一计数。
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { MAX_MENTIONS, seedUsers, peekUserId, knownUsernames } from '@/utils/mentionResolve'
import { MENTION_LEAD, hasMentionToken, extractMentionTokens } from '@/utils/mention'

// 正文结尾是否已是合法 @ 前导（MENTION_LEAD 的尾部形态；空串 = 行首，亦合法）
const LEAD_END_RE = new RegExp(`${MENTION_LEAD}$`)

export function useMentions({ getText, setText }) {
  const { t } = useI18n()
  const message = useMessage()
  const mentionedUsers = ref([])
  const selectedIds = computed(() => mentionedUsers.value.map(u => u.id))

  // 是否可选：未重复且未达上限（供 UI 置灰判断）
  const canSelect = user =>
    !!user?.id && !selectedIds.value.includes(user.id) && mentionedUsers.value.length < MAX_MENTIONS

  // 记录选择：重复忽略；超限警告并拒绝（后端也会静默截断到 10 人，此处提前拦截）
  const recordSelection = (user) => {
    if (!user?.id) return false
    if (selectedIds.value.includes(user.id)) return false
    if (mentionedUsers.value.length >= MAX_MENTIONS) {
      message.warning(t('notice.mention.limitTip'))
      return false
    }
    mentionedUsers.value.push(user)
    seedUsers([user])
    return true
  }

  // 按钮版选人：追加到正文末尾（原有交互，保持不变）
  const appendAtEnd = (user) => {
    if (!recordSelection(user)) return
    // @ 前导须满足 MENTION_LEAD 才能被识别为 token；结尾已是合法前导（行首/空白/
    // 左括号引号）则保留原间距，否则补一个空格，避免 @用户名 不被识别而被提交侧过滤
    const text = getText() || ''
    const lead = LEAD_END_RE.test(text) ? '' : ' '
    setText(`${text}${lead}@${user.username} `)
  }

  // 正文是唯一事实源：token 被删掉 → 同步移出已选（释放计数、弹窗恢复可选）；
  // token 出现且缓存里有 uuid（含手打名字）→ 反向补挂回名单。
  // 提交侧 filterMentionedIds 本就按正文过滤，两者语义对齐。
  watch(getText, (text) => {
    if (!text) {
      if (mentionedUsers.value.length) mentionedUsers.value = []
      return
    }
    const kept = mentionedUsers.value.filter(u => hasMentionToken(text, u.username))
    for (const name of extractMentionTokens(text, knownUsernames())) {
      if (kept.some(u => u.username === name)) continue
      const id = peekUserId(name)
      if (id) kept.push({ id, username: name })
    }
    if (kept.length !== mentionedUsers.value.length ||
        kept.some((u, i) => u !== mentionedUsers.value[i])) {
      mentionedUsers.value = kept
    }
  })

  // 提交成功/取消后清空（正文重置时同步调用）
  const clearMentioned = () => {
    mentionedUsers.value = []
  }

  return { mentionedUsers, selectedIds, canSelect, recordSelection, appendAtEnd, clearMentioned }
}
