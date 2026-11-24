/**
 * 用户关注（后端 v0.9.0）的常量与纯函数，不含文案（文案在 locales 的 follow.*）。
 * 接口封装见 api/follow.js。
 */
import {
  isBadRequestError,
  isForbiddenError,
  isNotFoundError,
  isUnauthorizedError,
  errorMessageIncludes
} from '@/utils/apiError'

// 当前登录用户与目标用户的关系（与后端枚举一致，勿改数值）
export const FOLLOW_RELATION = Object.freeze({
  NONE: 0, // 未关注（访客 / 看自己也是 0）
  FOLLOWING: 1, // 我关注了对方
  FOLLOWED_BY: 2, // 对方关注了我，我没关注对方
  MUTUAL: 3 // 互相关注
})

// relation → follow.relation.* 文案 key 后缀（按钮文案）
const RELATION_KEYS = {
  [FOLLOW_RELATION.NONE]: 'none',
  [FOLLOW_RELATION.FOLLOWING]: 'following',
  [FOLLOW_RELATION.FOLLOWED_BY]: 'followedBy',
  [FOLLOW_RELATION.MUTUAL]: 'mutual'
}

// 关注 / 粉丝列表可见性（GET/PUT /user/settings 的 follow_list_visibility）
export const FOLLOW_LIST_VISIBILITY = Object.freeze({
  PUBLIC: 0,
  PRIVATE: 1
})

// 列表页大小：默认 20，后端上限 50
export const FOLLOW_PAGE_SIZE = 20
export const FOLLOW_PAGE_SIZE_MAX = 50

/** 规范化 relation：非法值按 0 处理 */
export function normalizeRelation(relation) {
  return RELATION_KEYS[relation] !== undefined ? relation : FOLLOW_RELATION.NONE
}

/** 我是否已关注对方（1 / 3）：按钮点击应走取关 */
export function isFollowingRelation(relation) {
  return relation === FOLLOW_RELATION.FOLLOWING || relation === FOLLOW_RELATION.MUTUAL
}

/** 对方是否关注了我（2 / 3） */
export function isFollowedByRelation(relation) {
  return relation === FOLLOW_RELATION.FOLLOWED_BY || relation === FOLLOW_RELATION.MUTUAL
}

/** 关注按钮文案 key：follow.relation.none | following | followedBy | mutual */
export function relationLabelKey(relation) {
  return `follow.relation.${RELATION_KEYS[normalizeRelation(relation)]}`
}

/** 关注数达到上限（400，message 形如 following limit reached: ...） */
export function isFollowLimitError(error) {
  return isBadRequestError(error) && errorMessageIncludes(error, 'following limit reached')
}

/** 关注 / 取关自己（400 Cannot follow yourself） */
export function isFollowSelfError(error) {
  return isBadRequestError(error) && errorMessageIncludes(error, 'cannot follow yourself')
}

/** 对方把关注 / 粉丝列表设为私密（列表接口 403） */
export function isFollowListPrivateError(error) {
  return isForbiddenError(error)
}

/** 目标用户不存在 / 已禁用 / 已删除（stats、关注、列表 404） */
export function isFollowUserNotFoundError(error) {
  return isNotFoundError(error)
}

/**
 * 关注相关接口失败 → i18n key（不展示后端英文 message）。
 * 未登录（401）由 request 拦截器统一清 token 跳登录，这里仍给出文案以免调用方拿到 undefined。
 */
export function followErrorKey(error) {
  if (isFollowLimitError(error)) return 'follow.error.limitReached'
  if (isFollowSelfError(error)) return 'follow.error.self'
  if (isFollowListPrivateError(error)) return 'follow.error.listPrivate'
  if (isFollowUserNotFoundError(error)) return 'follow.error.userNotFound'
  if (isUnauthorizedError(error)) return 'follow.error.loginRequired'
  return 'follow.error.generic'
}
