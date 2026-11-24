// 关注按钮的状态与提交（后端 v0.9.0 POST / DELETE /follow/:user_id）。
//
// - 访客点击：弹登录引导，不发请求（避免 401 被拦截器清 token 硬跳转）
// - 请求进行中再点：直接忽略（按钮同时置 loading / disabled），快速重复点击只发一次
// - 成功：用响应里的 relation 更新状态（接口幂等，响应即最新关系），并广播给同页其他同一用户的按钮
// - 失败：状态不变，按 followErrorKey 提示
import { ref, watch, onScopeDispose, getCurrentScope } from 'vue'
import { followUser, unfollowUser } from '@/api/follow'
import { auth } from '@/utils/auth'
import { requireLogin } from '@/utils/guest-action'
import { FOLLOW_RELATION, isFollowingRelation, normalizeRelation, followErrorKey } from '@/utils/follow'

// 同一用户在页面上可能出现多次（列表 + 通知 + 主页）：关系变化后同步给所有订阅者
const listeners = new Set()

/**
 * 订阅关系变化。
 * @param {(e: { userId: string, relation: number, prev: number }) => void} cb
 * @returns {() => void} 取消订阅
 */
export function onFollowRelationChange(cb) {
  listeners.add(cb)
  return () => listeners.delete(cb)
}

function emitRelationChange(payload) {
  listeners.forEach(cb => {
    try {
      cb(payload)
    } catch (err) {
      console.error('follow relation listener failed:', err)
    }
  })
}

/**
 * @param {Object} options
 * @param {() => string} options.userId - 目标用户 ID（getter，便于 props 变化）
 * @param {() => number} [options.relation] - 外部传入的初始关系（getter；变化时同步）
 * @param {(e: { relation: number, prev: number }) => void} [options.onChange] - 本按钮操作成功后回调
 * @param {(key: string) => void} [options.onError] - 失败时回调，参数为 i18n key
 */
export function useFollowToggle({ userId, relation: relationGetter, onChange, onError } = {}) {
  const relation = ref(normalizeRelation(relationGetter?.() ?? FOLLOW_RELATION.NONE))
  const pending = ref(false)

  if (relationGetter) {
    watch(relationGetter, (v) => {
      // 进行中的请求结果优先：避免父组件旧值覆盖刚返回的新关系
      if (!pending.value) relation.value = normalizeRelation(v)
    })
  }

  // 别处（同一用户的其他按钮）操作成功后同步
  const off = onFollowRelationChange((e) => {
    if (e.userId === userId() && !pending.value) relation.value = e.relation
  })
  if (getCurrentScope()) onScopeDispose(off)

  const toggle = async () => {
    if (!auth.isAuthenticated()) {
      requireLogin('follow')
      return
    }
    const id = userId()
    if (!id || pending.value) return

    const prev = relation.value
    const unfollow = isFollowingRelation(prev)
    pending.value = true
    try {
      const res = await (unfollow ? unfollowUser(id) : followUser(id))
      const next = normalizeRelation(res?.data?.relation)
      relation.value = next
      onChange?.({ relation: next, prev })
      emitRelationChange({ userId: id, relation: next, prev })
    } catch (err) {
      onError?.(followErrorKey(err))
    } finally {
      pending.value = false
    }
  }

  return { relation, pending, toggle }
}
