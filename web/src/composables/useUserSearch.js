// 用户搜索组合式函数：MentionPicker（按钮版）与 MentionTrigger（编辑器内联版）共用。
// 内置单调序号丢弃过期响应、关键词变更即作废旧结果；
// 空 keyword 立即查询（弹层刚打开时的默认列表），有关键词才防抖。
// 支持后端 search_after 游标分页：loadMore 追加下一页，hasMore 标记是否还有更多。
// circleId 支持传值 / ref / getter（toValue 取值）：圈子内 @选人时传圈子 uuid，
// 圈子作用域变化时按当前 keyword 自动重查（如发帖页切换所属圈子）。
import { ref, toValue, watch } from 'vue'
import { searchUsers } from '@/api/user'
import { useDebounceFn } from '@/utils/throttle'

export function useUserSearch({ size = 10, delay = 300, circleId = null } = {}) {
  const keyword = ref('')
  const users = ref([])
  const loading = ref(false)
  const loadingMore = ref(false)
  const hasMore = ref(false)
  let searchAfter = ''
  let searchSeq = 0

  // 组装请求参数：圈子作用域仅在非空时携带（不传 = 全站搜索）
  const buildParams = () => {
    const params = { keyword: keyword.value, size }
    const cid = toValue(circleId)
    if (cid) params.circle_id = cid
    return params
  }

  // 应用响应：mode=replace（新搜索）或 append（翻页）；过期响应直接丢弃
  const apply = (res, seq, mode) => {
    if (seq !== searchSeq) return
    const page = res.data || {}
    const list = page.data || []
    users.value = mode === 'append' ? [...users.value, ...list] : list
    searchAfter = page.search_after || ''
    hasMore.value = !!searchAfter && list.length > 0
    loading.value = false
    loadingMore.value = false
  }

  const doSearch = async (kw) => {
    const seq = searchSeq
    loading.value = true
    // 新搜索开始即清掉翻页加载态：在途 loadMore 的响应稍后会被 apply 按序号丢弃，
    // 不在此重置会让 loadingMore 永远卡在 true（底部转圈不消失、后续无法翻页）；
    // loading 期间 loadMore 不会再启动，故这里同时覆盖了下方 catch 的错误路径
    loadingMore.value = false
    try {
      const res = await searchUsers(buildParams())
      apply(res, seq, 'replace')
    } catch (error) {
      if (seq !== searchSeq) return
      console.error('搜索用户失败:', error)
      users.value = []
      searchAfter = ''
      hasMore.value = false
      loading.value = false
    }
  }

  // 触底加载下一页：沿用当前 keyword + 上次返回的 search_after 游标
  const loadMore = async () => {
    if (loading.value || loadingMore.value || !hasMore.value) return
    loadingMore.value = true
    const seq = searchSeq
    try {
      const res = await searchUsers({
        ...buildParams(),
        search_after: searchAfter
      })
      apply(res, seq, 'append')
    } catch (error) {
      if (seq !== searchSeq) return
      console.error('加载更多用户失败:', error)
      hasMore.value = false
      loadingMore.value = false
    }
  }

  const debouncedSearch = useDebounceFn(doSearch, delay)

  // 关键词变化：作废旧结果 + 调度查询；空关键词立即查询（默认推荐列表）
  const search = (kw) => {
    keyword.value = kw
    searchSeq += 1
    users.value = []
    searchAfter = ''
    hasMore.value = false
    if (!kw) {
      doSearch(kw)
    } else {
      // 防抖等待期先置 loading，弹窗显示转圈而非残页
      loading.value = true
      debouncedSearch(kw)
    }
  }

  // 请求序号作废：迟到的旧响应直接丢弃
  const invalidate = () => {
    searchSeq += 1
    users.value = []
    loading.value = false
    loadingMore.value = false
    hasMore.value = false
    searchAfter = ''
    keyword.value = ''
  }

  // 圈子作用域变化（如发帖页切换所属圈子）：已有结果按新圈子重查，避免选中旧圈子可见的用户
  watch(() => toValue(circleId), (val, old) => {
    if (val === old) return
    if (keyword.value || users.value.length) search(keyword.value)
  })

  return { keyword, users, loading, loadingMore, hasMore, search, loadMore, invalidate }
}
