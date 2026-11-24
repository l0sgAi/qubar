// @提及正文匹配工具：选人时正文插入「@用户名 」展示文本，提交前需确认
// 正文中仍存在完整的 @用户名 token，才把对应 uuid 传给后端。
//
// 边界规则多处共用（本文件 / 编辑器内联弹窗 MentionTrigger / 编辑高亮 mentionHighlight /
// 渲染侧链接化 mentionDom 的空格感知扫描），任何一侧调整必须同步：
// - 前导：行首或空白/常见左括号引号。字母数字与 . / : @ 均不可作前导，
//   否则邮箱 xiaoming@example.com 会误匹配出 token「example」。
// - 后置：@用户名 之后不能再出现用户名可含字符或另一个 @，
//   否则 @alice 会误中 @alice2。

const escapeRegExp = s => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

// 用户名字符集：字母数字下划线/连字符/中日韩统一表意文字
export const NAME_CHARS = String.raw`[A-Za-z0-9_一-鿿-]`
// 合法前导字符集合（捕获组 1 = 前导字符，行首时为空串）
export const MENTION_LEAD = String.raw`(^|[\s(\[{>"'“‘《【])`
// 完整用户名长度上限
export const MENTION_NAME_MAX = 30
// 编辑器内 @ 后允许的最长搜索关键词
export const MENTION_KEYWORD_MAX = 20

// 渲染侧完整 token（g 标志逐个 exec；捕获组 1=前导 组 2=用户名）
export const MENTION_FULL_RE = new RegExp(
  `${MENTION_LEAD}@(${NAME_CHARS}{1,${MENTION_NAME_MAX}})(?!${NAME_CHARS}|@)`,
  'g'
)
// 编辑侧 non-global：光标前文本须以「可选前导 + @ + 关键词」结尾
export const MENTION_TAIL_RE = new RegExp(`(?:${MENTION_LEAD})@(${NAME_CHARS}{0,${MENTION_KEYWORD_MAX}})$`)

// 仅由 NAME_CHARS 构成的名字：通用分支已能匹配，无须进已知名 alternation
const NAME_ONLY_RE = new RegExp(`^${NAME_CHARS}{1,${MENTION_NAME_MAX}}$`)

// 已知完整用户名（可含空格等 NAME_CHARS 之外的字符）优先匹配：
// 带空格的名字无法被通用字符集分支覆盖，把选中/缓存里的这类名字面量纳入
// alternation（长名优先，防止 "John Doe" 被 "John" 语义遮蔽），通用分支兜底。
// 单例缓存：按已知名集合内容失效，渲染热路径（逐行扫描）免重建。
let knownReKey = ''
let knownRe = null
export const getMentionFullRe = (knownNames = []) => {
  const spaced = [...new Set(knownNames)]
    .map(n => String(n || ''))
    .filter(n => n && !NAME_ONLY_RE.test(n))
    .sort((a, b) => b.length - a.length)
  if (!spaced.length) return MENTION_FULL_RE
  const key = spaced.join('\u0000')
  if (key === knownReKey) return knownRe
  knownRe = new RegExp(
    `${MENTION_LEAD}@(${spaced.map(escapeRegExp).join('|')}|${NAME_CHARS}{1,${MENTION_NAME_MAX}})(?!${NAME_CHARS}|@)`,
    'g'
  )
  knownReKey = key
  return knownRe
}

// 完整 token 匹配：确认正文中仍存在完整的 @用户名
export const hasMentionToken = (content, username) => {
  if (!content || !username) return false
  return new RegExp(`${MENTION_LEAD}@${escapeRegExp(username)}(?!${NAME_CHARS}|@)`).test(content)
}

// 提取正文中所有完整 @用户名（去重、保持出现顺序），渲染侧用于回查 uuid。
// knownNames：已知的带空格等完整用户名，优先整名匹配
export const extractMentionTokens = (text, knownNames = []) => {
  if (!text) return []
  const re = getMentionFullRe(knownNames)
  re.lastIndex = 0
  const names = []
  let m
  while ((m = re.exec(text)) !== null) {
    if (!names.includes(m[2])) names.push(m[2])
    // 零宽匹配死循环保险
    if (m.index === re.lastIndex) re.lastIndex += 1
  }
  return names
}

// 过滤出正文中仍存在的提及，返回用户 id 列表
export const filterMentionedIds = (content, users) =>
  users.filter(u => hasMentionToken(content, u.username)).map(u => u.id)
