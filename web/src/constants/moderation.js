/**
 * 圈内内容审核枚举常量与 AI 结论解析（纯函数，不含文案）。
 * 文案见 locales 的 circle.moderation.*（风险 key → circle.moderation.hazards.<key>，
 * 危害等级 → circle.moderation.severity.<level>）。
 */

// 风险 key（与后端 AI 审核结论 hazards 的 key 一致，顺序即全量表展示顺序）
export const HAZARD_KEYS = [
  'sexual',
  'violence_terror',
  'harassment',
  'illegal',
  'spam_ad',
  'privacy',
  'self_harm',
  'manipulation'
]

// 需要额外提示「可能需要人工关怀」的风险 key
export const CARE_HAZARD = 'self_harm'

// 危害程度等级：severity 为 0–3 的期望分，四舍五入后落到下列等级
export const SEVERITY_LEVELS = ['none', 'minor', 'severe', 'extreme']

// 审核动作（POST /circle/moderation/review 的 action）
export const REVIEW_ACTION = {
  APPROVE: 'approve',
  REJECT: 'reject'
}

// 审核备注上限（后端超出截断，前端直接限制输入）
export const REVIEW_REASON_MAX = 200

// 待审列表单页大小（后端 ≤0 或 >100 按 20 处理）；页头角标也只拉这一页，满页显示「20+」
export const MODERATION_PAGE_SIZE = 20

/** 概率（0–1）转整数百分比，非法值返回 0 */
export function toPercent(probability) {
  const n = Number(probability)
  if (!Number.isFinite(n)) return 0
  return Math.round(Math.min(Math.max(n, 0), 1) * 100)
}

/** severity 期望分（0–3）→ SEVERITY_LEVELS 中的等级；null / 非数值返回 null */
export function severityLevel(severity) {
  if (severity === null || severity === undefined || severity === '') return null
  const n = Number(severity)
  if (!Number.isFinite(n)) return null
  const index = Math.min(Math.max(Math.round(n), 0), SEVERITY_LEVELS.length - 1)
  return SEVERITY_LEVELS[index]
}

/**
 * 解析 AI 审核结论的展示状态：
 * - 'pending'：ai_verdict 为 null，AI 尚未处理，可直接人工处理
 * - 'failed'：error_msg 非空，AI 服务不可用，未能给出结论
 * - 'ready'：正常结论
 */
export function verdictState(verdict) {
  if (!verdict) return 'pending'
  if (verdict.error_msg) return 'failed'
  return 'ready'
}

/** 命中风险是否包含需要人工关怀的 self_harm */
export function needsCare(verdict) {
  return Array.isArray(verdict?.hit_hazards) && verdict.hit_hazards.includes(CARE_HAZARD)
}

// AI 结论 decision 中有展示文案的取值（circle.moderation.decision.<key>）；
// 被举报列表常见 publish / review_publish / fail_open，待审列表的 review 不单独标注
export const VERDICT_DECISIONS = ['publish', 'review_publish', 'fail_open']

// 举报复核动作（POST /circle/moderation/reports/resolve 的 action）
export const RESOLVE_ACTION = {
  DISMISS: 'dismiss',
  REMOVE: 'remove'
}

// 被举报列表每帖最多返回的举报条数（最近 10 条，新→旧）；report_count 超过时注明「仅显示最近 10 条」
export const REPORTS_SHOWN_MAX = 10

/** decision → 文案 key；无文案的取值（含 review / 空）返回 null */
export function decisionKey(decision) {
  return VERDICT_DECISIONS.includes(decision) ? decision : null
}

// 通知跳转 ?focus= 定位被举报帖子时最多翻的页数（含首页），仍找不到视为已被他人处理
export const FOCUS_MAX_PAGES = 3

/**
 * 解析 summary 接口（#156）响应为 { reported, pending }；字段缺失 / 非数值返回 null（按接口不可用处理）
 */
export function parseModerationSummary(data) {
  const { report_pending: reported, review_pending: pending } = data || {}
  if (!Number.isFinite(reported) || !Number.isFinite(pending)) return null
  return { reported: Math.max(reported, 0), pending: Math.max(pending, 0) }
}
