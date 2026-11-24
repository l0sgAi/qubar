/**
 * 圈子模块枚举常量与权限判断（纯函数，不含文案）。
 * 文案映射见 src/composables/useCircleMeta.js（依赖 i18n）。
 */

// 成员角色（与后端枚举一致，勿改数值）
export const CIRCLE_ROLE = {
  MEMBER: 10,
  ADMIN: 20,
  OWNER: 30
}

// 成员状态（0 待审核已随入圈审核下线，后端 #168；存量已迁移为 4，后端不再返回）
export const MEMBER_STATUS = {
  ACTIVE: 1, // 正常在圈
  MUTED: 2, // 禁言中（仍是成员）
  BANNED: 3, // 已拉黑（已不在圈）
  EXITED: 4 // 已退出（主动退圈、被移出或解除拉黑后的落点）
}

// 加入方式：只剩公开 / 私密（邀请制）。1（需审核）已下线，存量已迁移为 2，创建 / 编辑传 1 返回 400
export const JOIN_TYPE = {
  DIRECT: 0,
  PRIVATE: 2
}

/**
 * 归一化加入方式：非 0 一律按私密处理。
 * 后端圈子缓存（TTL 24h）在迁移后仍可能返回已下线的 1，表单与展示都不能再出现「需审核」。
 */
export function normalizeJoinType(joinType) {
  return joinType === JOIN_TYPE.DIRECT || joinType === undefined || joinType === null
    ? JOIN_TYPE.DIRECT
    : JOIN_TYPE.PRIVATE
}

/** 是否私密圈（含已下线的 1） */
export function isPrivateJoinType(joinType) {
  return normalizeJoinType(joinType) === JOIN_TYPE.PRIVATE
}

/**
 * 私密圈非成员（含访客）不显示可点击的加入按钮：POST /circle/join 对私密圈一律 403，
 * 只能拿邀请链接走落地页加入（后端 #169），这里只展示「需邀请加入」提示。
 */
export function needsInviteToJoin(circle) {
  return !!circle && !circle.is_joined && isPrivateJoinType(circle.join_type)
}

/**
 * POST /circle/join 失败 → i18n key（不展示后端英文 message）。
 * 信封 code 与 HTTP 状态一一对应（203↔403、204↔404、207↔409），两者都认。
 * 403 两种原因（私密圈需邀请 / 已被拉黑）码相同，按圈子加入方式区分：公开圈只有拉黑会 403。
 */
export function joinErrorKey(error, joinType) {
  const code = error?.code
  const status = error?.status
  if (code === 207 || code === 409 || status === 409) return 'circle.joinError.alreadyMember'
  if (code === 203 || code === 403 || status === 403) {
    return isPrivateJoinType(joinType) ? 'circle.joinError.inviteOnly' : 'circle.joinError.banned'
  }
  if (code === 204 || status === 404) return 'circle.joinError.notFound'
  return 'circle.joinError.generic'
}

/** 私密圈非成员访问圈内帖子：GET /circle/posts 返回 403 */
export function isForbiddenError(error) {
  return error?.code === 203 || error?.code === 403 || error?.status === 403
}

// PUT /circle/update 清除 category_id 时传的全零 UUID
export const EMPTY_CATEGORY_ID = '00000000-0000-0000-0000-000000000000'

// 禁言时长上限（小时）：1 小时 ~ 30 天
export const MUTE_DURATION_MIN = 1
export const MUTE_DURATION_MAX = 720

// 成员列表单页大小（后端 ≤0 或 >100 按 20 处理）
export const MEMBER_PAGE_SIZE = 20

/** 是否为管理侧角色（圈主或管理员） */
export function isManager(role) {
  return role === CIRCLE_ROLE.ADMIN || role === CIRCLE_ROLE.OWNER
}

/** 是否圈主 */
export function isOwner(role) {
  return role === CIRCLE_ROLE.OWNER
}

/**
 * 通用操作规则：只能操作角色严格低于自己的人（圈主 > 管理员 > 成员）。
 * 目标角色 ≥ 自己（含自己）一律不可操作。
 */
export function canTarget(myRole, targetRole) {
  return myRole > targetRole
}

/**
 * 禁言/解禁/移出/拉黑/解黑的额外限制：
 * 管理员仅可对普通成员(10)操作，圈主不受限（仍需满足 canTarget）。
 */
export function canModerate(myRole, targetRole) {
  if (!canTarget(myRole, targetRole)) return false
  if (myRole === CIRCLE_ROLE.ADMIN) return targetRole === CIRCLE_ROLE.MEMBER
  return true
}
