import { ref } from 'vue'
import { getMyCircles, getActiveCircles, getRandomCircles } from '@/api/post'
import { auth } from '@/utils/auth'

// 侧栏圈子数据共享缓存（模块级单例）。
// 背景：SideNav 在每个页面视图里独立挂载，若状态放在组件内，每次路由切换都会
// 重新挂载并全量请求 /circle/my、/circle/active、/circle/random，后端压力随
// 页面跳转线性放大。把数据提升到模块作用域后，路由切换只复用缓存，仅在以下
// 时机重新请求：
//   1. 首次挂载（缓存为空）
//   2. 侧栏内操作（新建圈子成功等）显式调用 refresh*
//   3. 加入/退出圈子后广播 MEMBERSHIP_CHANGED_EVENT（见 CircleDetail）
//   4. 登录态变化（token 变了说明切账号/登出，登录态数据整体失效）

// 加入/退出圈子后由页面广播的事件名，侧栏监听后刷新「我的圈子」
export const MEMBERSHIP_CHANGED_EVENT = 'qubar:circle-membership-changed'

const joinedCircles = ref([])
const activeCircles = ref([])
const randomCircles = ref([])
const joinedLoading = ref(false)
const activeLoading = ref(false)
const randomLoading = ref(false)

let joinedLoaded = false
let activeLoaded = false
let randomLoaded = false
// 缓存归属的 token：与当前 token 不一致时登录态缓存整体作废
let cacheToken = null
// 拉取「我的圈子」期间又收到成员变更事件时置位，请求结束后补一次刷新
let joinedRefreshPending = false

const mapCircle = (c, fallbackName) => ({
  id: c.id,
  name: c.name || fallbackName,
  avatar: c.avatar_url || ''
})

// 校验缓存归属：切账号/登出后清空登录态缓存，避免展示上一个账号的圈子
const checkCacheOwner = () => {
  const token = auth.getToken()
  if (token !== cacheToken) {
    cacheToken = token
    joinedCircles.value = []
    activeCircles.value = []
    joinedLoaded = false
    activeLoaded = false
  }
}

export const fetchJoinedCircles = async (fallbackName) => {
  // 正在拉取时收到刷新请求：记录待刷新，当前请求结束后补一次
  if (joinedLoading.value) {
    joinedRefreshPending = true
    return
  }
  joinedLoading.value = true
  // 请求前记录 token，响应返回时若登录态已变则丢弃，避免写入上个账号的数据
  const requestToken = auth.getToken()
  try {
    const res = await getMyCircles({ size: 5 })
    if (requestToken !== auth.getToken()) return
    joinedCircles.value = (res.data?.circles || []).map(c => mapCircle(c, fallbackName)).slice(0, 5)
    joinedLoaded = true
  } catch (e) {
    console.error('获取我加入的圈子失败:', e)
    // 保留上次成功的列表，仅复位 loaded 标记让 ensureSideNavData 可重试
    joinedLoaded = false
  } finally {
    joinedLoading.value = false
    if (requestToken !== auth.getToken()) {
      // 请求期间登录态变化：结果已丢弃，为当前登录态重新拉取
      joinedRefreshPending = false
      if (auth.isAuthenticated()) fetchJoinedCircles(fallbackName)
    } else if (joinedRefreshPending) {
      joinedRefreshPending = false
      fetchJoinedCircles(fallbackName)
    }
  }
}

export const fetchActiveCircles = async (fallbackName) => {
  if (activeLoading.value) return
  activeLoading.value = true
  // 请求前记录 token，响应返回时若登录态已变则丢弃，避免写入上个账号的数据
  const requestToken = auth.getToken()
  try {
    const res = await getActiveCircles({ size: 5, offset: 0 })
    if (requestToken !== auth.getToken()) return
    activeCircles.value = (res.data?.circles || []).map(c => mapCircle(c, fallbackName)).slice(0, 5)
    activeLoaded = true
  } catch (e) {
    console.error('获取近期活跃圈子失败:', e)
    // 保留上次成功的列表，仅复位 loaded 标记让 ensureSideNavData 可重试
    activeLoaded = false
  } finally {
    activeLoading.value = false
    // 请求期间登录态变化：结果已丢弃，为当前登录态重新拉取
    if (requestToken !== auth.getToken() && auth.isAuthenticated()) {
      fetchActiveCircles(fallbackName)
    }
  }
}

export const fetchRandomCircles = async (fallbackName) => {
  if (randomLoading.value) return
  randomLoading.value = true
  try {
    const res = await getRandomCircles({ size: 20 })
    randomCircles.value = (res.data?.circles || []).map(c => mapCircle(c, fallbackName))
    randomLoaded = true
  } catch (e) {
    console.error('获取随机圈子失败:', e)
    // 保留上次成功的列表，仅复位 loaded 标记让 ensureSideNavData 可重试
    randomLoaded = false
  } finally {
    randomLoading.value = false
  }
}

// 挂载时调用：只补未加载的分区，已有缓存直接复用
export const ensureSideNavData = (fallbackName) => {
  checkCacheOwner()
  // 匿名态（如发现页落地）不拉取登录态圈子，避免 /circle/my、/circle/active 触发 401 重定向
  if (auth.isAuthenticated()) {
    if (!joinedLoaded) fetchJoinedCircles(fallbackName)
    if (!activeLoaded) fetchActiveCircles(fallbackName)
  }
  // 随机圈子无需登录，访客也可展示
  if (!randomLoaded) fetchRandomCircles(fallbackName)
}

// 加入/退出圈子后刷新「我的圈子」（由事件监听触发，也可手动调用）
export const refreshJoinedCircles = (fallbackName) => {
  if (auth.isAuthenticated()) fetchJoinedCircles(fallbackName)
}

let membershipListenerBound = false

export function useSideNavCircles() {
  // 事件监听只需绑定一次（模块级），避免每个 SideNav 实例重复注册
  if (!membershipListenerBound && typeof window !== 'undefined') {
    membershipListenerBound = true
    window.addEventListener(MEMBERSHIP_CHANGED_EVENT, () => {
      refreshJoinedCircles()
    })
  }

  return {
    joinedCircles,
    activeCircles,
    randomCircles,
    circlesLoading: joinedLoading,
    activeCirclesLoading: activeLoading,
    randomCirclesLoading: randomLoading,
    ensureSideNavData,
    refreshJoinedCircles,
    refreshActiveCircles: (fallbackName) => auth.isAuthenticated() && fetchActiveCircles(fallbackName),
    refreshRandomCircles: fetchRandomCircles
  }
}
