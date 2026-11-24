/**
 * 机器人自动回复配置（auto_config.auto_reply）的表单工具
 *
 * 字段与校验规则以后端文档为准：docs/api/ai-agent-admin-api.md §2.5 / §3.4
 * （全局 /agent 与圈内 /circle/agent 共用同一套规则）。
 * 更新时 auto_reply 是整体替换语义：传了就覆盖整个子对象、未传字段按 0 / false 处理，
 * 所以提交时总是带上完整对象。
 */

// 目标类型（顺序即表单展示与提交顺序）
export const AUTO_REPLY_TARGETS = ['post', 'comment']

// 取值范围，与后端 buildAutoReply 一致
export const AUTO_REPLY_WINDOW_HOURS_MAX = 168
export const AUTO_REPLY_MAX_PER_HOUR_MAX = 1000
export const AUTO_REPLY_THRESHOLD_MAX = 2 ** 30

// 首次开启（从未配置过）时预填的时间窗口
const DEFAULT_WINDOW_HOURS = 24

const NUMBER_FIELDS = [
  'post_hot_min', 'comment_like_min', 'comment_reply_min', 'window_hours', 'max_per_hour'
]

/** 未配置时的零值（与后端回显一致） */
export const emptyAutoReply = () => ({
  enabled: false,
  targets: [],
  post_hot_min: 0,
  comment_like_min: 0,
  comment_reply_min: 0,
  window_hours: 0,
  max_per_hour: 0
})

// 数字输入框清空后为 null：按 0 处理
const toInt = v => (typeof v === 'number' && Number.isFinite(v) ? v : 0)

/**
 * 规范化为完整的 auto_reply 对象：补齐缺省字段、空值转 0、targets 去重并按固定顺序排列。
 * 非法 target 原样保留（交给 validateAutoReply 报错），不静默丢弃。
 * @param {Object} [cfg] - AgentVO.auto_config.auto_reply 或表单值
 * @returns {Object} 完整 auto_reply 对象
 */
export const normalizeAutoReply = cfg => {
  const out = emptyAutoReply()
  if (!cfg || typeof cfg !== 'object') return out
  out.enabled = cfg.enabled === true
  const targets = Array.isArray(cfg.targets) ? cfg.targets : []
  const known = AUTO_REPLY_TARGETS.filter(t => targets.includes(t))
  const unknown = targets.filter(t => !AUTO_REPLY_TARGETS.includes(t))
  out.targets = [...known, ...unknown]
  for (const k of NUMBER_FIELDS) out[k] = toInt(cfg[k])
  return out
}

/**
 * 从 AgentVO 读出表单用的 auto_reply（未配置 / 旧后端无该字段时返回零值）
 * @param {Object} [agent] - AgentVO
 * @returns {Object}
 */
export const autoReplyFromAgent = agent => normalizeAutoReply(agent?.auto_config?.auto_reply)

/**
 * 开启自动回复时预填：从未配置过的机器人默认回复帖子、看最近 24 小时；
 * 门槛不预填（热度量级因站点而异），交给运营填写。
 * @param {Object} cfg - 表单值（原地修改）
 */
export const prefillOnEnable = cfg => {
  if (!cfg.targets?.length) cfg.targets = ['post']
  if (!cfg.window_hours) cfg.window_hours = DEFAULT_WINDOW_HOURS
}

const inRange = (v, max) => Number.isInteger(v) && v >= 0 && v <= max

/**
 * 按后端规则校验，返回 { 字段: i18n key }，无错误时为空对象。
 * 字段名 comment_threshold 表示两个评论门槛的联合规则。
 * @param {Object} cfg - 表单值
 * @returns {Object<string, string>}
 */
export const validateAutoReply = cfg => {
  const c = normalizeAutoReply(cfg)
  const errors = {}
  const raw = Array.isArray(cfg?.targets) ? cfg.targets : []
  if (raw.some(t => !AUTO_REPLY_TARGETS.includes(t)) || new Set(raw).size !== raw.length) {
    errors.targets = 'agent.autoReply.errTargetsInvalid'
  }
  // 范围校验：不论是否开启都生效
  for (const k of ['post_hot_min', 'comment_like_min', 'comment_reply_min']) {
    if (!inRange(c[k], AUTO_REPLY_THRESHOLD_MAX)) errors[k] = 'agent.autoReply.errThresholdRange'
  }
  if (!inRange(c.window_hours, AUTO_REPLY_WINDOW_HOURS_MAX)) {
    errors.window_hours = 'agent.autoReply.errWindowRange'
  }
  if (!inRange(c.max_per_hour, AUTO_REPLY_MAX_PER_HOUR_MAX)) {
    errors.max_per_hour = 'agent.autoReply.errMaxPerHourRange'
  }
  if (!c.enabled) return errors

  // 开启时：至少一个目标、窗口必填、每个目标都要有生效门槛
  if (!c.targets.length) errors.targets ??= 'agent.autoReply.errTargetsRequired'
  if (c.window_hours < 1) errors.window_hours ??= 'agent.autoReply.errWindowRange'
  if (c.targets.includes('post') && c.post_hot_min < 1) {
    errors.post_hot_min ??= 'agent.autoReply.errPostHotRequired'
  }
  if (c.targets.includes('comment') && c.comment_like_min < 1 && c.comment_reply_min < 1) {
    errors.comment_threshold = 'agent.autoReply.errCommentThresholdRequired'
  }
  return errors
}

/**
 * 判断表单值相对机器人当前配置是否有变化（编辑时决定是否提交 auto_config）
 * @param {Object} cfg - 表单值
 * @param {Object} [agent] - AgentVO
 * @returns {boolean}
 */
export const autoReplyChanged = (cfg, agent) =>
  JSON.stringify(normalizeAutoReply(cfg)) !== JSON.stringify(autoReplyFromAgent(agent))

/**
 * 组装请求体里的 auto_config（整体替换语义：始终带完整 auto_reply 对象）
 * @param {Object} cfg - 表单值
 * @returns {{ auto_reply: Object }}
 */
export const buildAutoConfig = cfg => ({ auto_reply: normalizeAutoReply(cfg) })
