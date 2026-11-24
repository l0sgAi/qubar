<template>
  <div class="app-header">
    <div class="header-left">
      <SmartLink class="logo" :to="homeTarget">
        <img src="/favicon.svg" alt="logo" class="logo-icon" />
        <span class="logo-text">{{ t('common.appName') }}</span>
      </SmartLink>
    </div>

    <div class="header-center">
      <!-- 搜索栏 -->
      <div class="search-wrapper">
        <div class="search-box">
          <!-- 圈子标签 -->
          <div v-if="circleSearch.id" class="circle-tag">
            <img v-if="circleSearch.avatarUrl" :src="circleSearch.avatarUrl" class="circle-tag-avatar" />
            <div v-else class="circle-tag-avatar-placeholder">
              {{ circleSearch.name?.charAt(0)?.toUpperCase() || '?' }}
            </div>
            <span class="circle-tag-name">{{ circleSearch.name }}</span>
            <button class="circle-tag-close" @click="handleClearCircleSearch" type="button">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="18" y1="6" x2="6" y2="18"></line>
                <line x1="6" y1="6" x2="18" y2="18"></line>
              </svg>
            </button>
          </div>
          <span class="search-icon" @click="handleSearch" style="cursor: pointer;">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="11" cy="11" r="8"></circle>
              <path d="m21 21-4.35-4.35"></path>
            </svg>
          </span>
          <input
            ref="searchInputRef"
            type="text"
            class="search-input"
            :placeholder="t('common.searchPlaceholder')"
            v-model="searchKeyword"
            @focus="showSuggestions = true"
            @keyup.enter="handleSearch"
          />
        </div>
        <!-- 搜索建议下拉菜单 -->
        <Transition name="suggestion-fade">
          <div
            v-if="showSuggestions && searchKeyword.trim()"
            class="search-suggestions"
          >
            <!-- 搜索建议：真实链接可右键新标签页；mousedown.prevent 保持输入框焦点 -->
            <SmartLink
              class="suggestion-item"
              :to="{ path: '/search', query: suggestionQuery('post') }"
              @mousedown.prevent
              @click="showSuggestions = false"
            >
              <svg class="suggestion-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
                <polyline points="14 2 14 8 20 8"></polyline>
                <line x1="16" y1="13" x2="8" y2="13"></line>
                <line x1="16" y1="17" x2="8" y2="17"></line>
              </svg>
              <span class="suggestion-text">{{ t('common.searchPosts') }}：{{ searchKeyword.trim() }}</span>
            </SmartLink>
            <SmartLink
              class="suggestion-item"
              :to="{ path: '/search', query: suggestionQuery('circle') }"
              @mousedown.prevent
              @click="showSuggestions = false"
            >
              <svg class="suggestion-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="10"></circle>
                <circle cx="12" cy="12" r="3"></circle>
              </svg>
              <span class="suggestion-text">{{ t('common.searchCircles') }}：{{ searchKeyword.trim() }}</span>
            </SmartLink>
            <SmartLink
              class="suggestion-item"
              :to="{ path: '/search', query: suggestionQuery('user') }"
              @mousedown.prevent
              @click="showSuggestions = false"
            >
              <svg class="suggestion-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"></path>
                <circle cx="12" cy="7" r="4"></circle>
              </svg>
              <span class="suggestion-text">{{ t('common.searchUsers') }}：{{ searchKeyword.trim() }}</span>
            </SmartLink>
          </div>
        </Transition>
      </div>
    </div>

    <div class="header-right">
      <!-- 语言切换器 -->
      <LanguageSwitcher />

      <!-- 匿名态：显示「登录」按钮（发现页等公开页落地用） -->
      <NButton v-if="!isLoggedIn" size="large" type="primary" class="login-btn" @click="goLogin">
        {{ t('login.tabs.login') }}
      </NButton>

      <!-- 登录态：发帖 + 消息 + 头像下拉 -->
      <template v-else>
      <!-- 消息中心铃铛 -->
      <div class="notification-wrapper">
        <!-- 发帖按钮 -->
      <NButton
      size="large"
      quaternary
      class="notification-btn"
      @click="handleCreatePost">
            <NIcon size="20">
            <AddIcon />
            </NIcon>
        <div style="margin-left: 5px;">{{ t('post.createPost') }}</div>
      </NButton>

        <NButton
          quaternary
          circle
          size="large"
          class="notification-btn"
          @click="handleNotification"
        >
          <NIcon size="20">
            <Bell />
            </NIcon>
        </NButton>
        <NBadge :value="notificationCount" :max="99" class="notification-badge" />
      </div>

      <!-- 私信入口：角标为私信未读总数（dm-unread），与消息中心铃铛分开 -->
      <div class="notification-wrapper">
        <NButton
          quaternary
          circle
          size="large"
          class="notification-btn"
          :aria-label="t('message.entry')"
          :title="t('message.entry')"
          @click="handleMessages"
        >
          <NIcon size="20">
            <MessageCircle />
          </NIcon>
        </NButton>
        <NBadge :value="dmUnreadCount" :max="99" class="notification-badge" />
      </div>

      <!-- 用户信息下拉菜单 -->
      <AppDropdown :options="userMenuOptions" @select="handleMenuSelect">
        <div class="user-info-trigger">
          <NAvatar
            round
            :size="40"
            :src="userAvatarUrl"
            class="user-avatar">
            <div v-if="userAvatarUrl===undefined || userAvatarUrl==''" >{{ username.charAt(0) }}</div>
          </NAvatar>
        </div>
      </AppDropdown>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, h, onMounted, onBeforeUnmount, watch, inject } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NButton, NIcon, NAvatar, NBadge, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { auth } from '@/utils/auth'
import { AddCircleOutlineFilled as AddIcon } from '@vicons/material'
import { Bell, MessageCircle } from '@vicons/tabler'
import request from '@/utils/request'
import LanguageSwitcher from '@/components/common/LanguageSwitcher.vue'
import AppDropdown from '@/components/common/AppDropdown.vue'
import SmartLink from '@/components/common/SmartLink.vue'
import { useUnreadNotice } from '@/composables/useUnreadNotice'
import { useLogout } from '@/composables/useLogout'

const router = useRouter()
const route = useRoute()
const message = useMessage()
const { t } = useI18n()

// 注入圈子搜索状态
const circleSearch = inject('circleSearchState', ref({ id: null, name: '', avatarUrl: '' }))
const clearCircleSearch = inject('clearCircleSearch', () => {})

// 用户信息
const username = ref('User')
const userAvatarUrl = ref('')

// 未读数：SSE 实时推送为主、30s 轮询降级。连接与本地校正事件（notice-read/notice-read-all）
// 由 useUnreadNotice 单例统一管理（挂载即 acquire、卸载即 release），移动端 TabBar 共用同一份
const { unreadCount: notificationCount, dmUnreadCount } = useUnreadNotice({
  onAuthExpired: () => {
    // 服务端检测到登出/过期/被踢下线（token 已在 composable 内清除）
    message.warning(t('common.logout'))
    router.push('/')
  }
})

// 是否登录（匿名态如发现页落地时，顶栏改显「登录」按钮）
const isLoggedIn = computed(() => auth.isAuthenticated())

// 搜索关键词
const searchKeyword = ref('')
const showSuggestions = ref(false)
const searchInputRef = ref(null)

// 点击外部关闭建议菜单
const handleClickOutside = (e) => {
  if (!e.target.closest('.search-wrapper')) {
    showSuggestions.value = false
  }
}

// 用户头像首字母
const userInitial = computed(() => {
  return username.value ? username.value.charAt(0).toUpperCase() : 'U'
})

// 获取用户信息
const fetchUserInfo = async () => {
    const response = await request.get('/user/get')
    if (response.data) {
      const userData = response.data
      username.value = userData.username || 'User'
      userAvatarUrl.value = userData.avatar_url || ''
    }
}

// 组装url
const getAvatarUrl = (name) => {
  return 'https://ui-avatars.com/api/?name='+name
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)

  if (auth.isAuthenticated()) {
    fetchUserInfo()
  }
  // 同步搜索框内容与当前路由
  if (route.name === 'search' && route.query.q) {
    searchKeyword.value = route.query.q
  }
})

onBeforeUnmount(() => {
  document.removeEventListener('click', handleClickOutside)
})

// 监听路由变化，同步搜索框内容
watch(() => route.query.q, (newQ) => {
  if (route.name === 'search' && newQ) {
    searchKeyword.value = newQ
  }
})

// 用户下拉菜单选项
const userMenuOptions = computed(() => [
  {
    label: t('user.profile'),
    key: 'profile',
    icon: () => h('svg', {
      viewBox: '0 0 24 24',
      fill: 'none',
      stroke: 'currentColor',
      'stroke-width': '2',
      style: { width: '16px', height: '16px' }
    }, [
      h('path', { d: 'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2' }),
      h('circle', { cx: '12', cy: '7', r: '4' })
    ])
  },
  {
    type: 'divider',
    key: 'd1'
  },
  {
    label: t('common.logout'),
    key: 'logout',
    icon: () => h('svg', {
      viewBox: '0 0 24 24',
      fill: 'none',
      stroke: 'currentColor',
      'stroke-width': '2',
      style: { width: '16px', height: '16px' }
    }, [
      h('path', { d: 'M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4' }),
      h('polyline', { points: '16 17 21 12 16 7' }),
      h('line', { x1: '21', y1: '12', x2: '9', y2: '12' })
    ])
  }
])

// 处理菜单选择
const handleMenuSelect = (key) => {
  switch (key) {
    case 'profile':
      handleProfile()
      break
    case 'logout':
      handleLogout()
      break
  }
}

// 个人中心
const handleProfile = () => {
  router.push('/profile')
}

// 退出登录（确认 + 断开未读推送 + 清 token，逻辑见 useLogout，与移动端「我的」页共用）
const { confirmLogout: handleLogout } = useLogout()

// 发帖
const handleCreatePost = () => {
  router.push('/create-post')
}

// 消息中心
const handleNotification = () => {
  router.push('/notifications')
}

// 私信会话列表
const handleMessages = () => {
  router.push('/messages')
}

// 返回主页（匿名态回到发现页，避免被 /home 的登录守卫弹走）。
// AppHeader 随视图挂载，登录跳转后重新求值，普通常量即可拿到最新登录态
const homeTarget = auth.isAuthenticated() ? '/home' : '/discover'

// 匿名态点击「登录」
const goLogin = () => {
  router.push('/')
}

// 搜索
const handleSearch = () => {
  const keyword = searchKeyword.value.trim()
  if (keyword) {
    showSuggestions.value = false
    // 构建查询参数
    const query = { q: keyword }

    // 如果有圈子搜索状态，添加 circle_id 参数
    if (circleSearch.value.id) {
      query.circle_id = circleSearch.value.id
    }

    // 如果已经在搜索页面且搜索相同关键词，需要强制刷新
    if (route.name === 'search' && route.query.q === keyword) {
      router.replace({
        path: '/search',
        query: { ...query, t: Date.now() }
      })
    } else {
      router.push({
        path: '/search',
        query
      })
    }
  }
}

// 搜索建议跳转参数（q + tab，命中圈子搜索态时附带 circle_id）
const suggestionQuery = (tab) => {
  const query = { q: searchKeyword.value.trim(), tab }
  if (circleSearch.value.id) {
    query.circle_id = circleSearch.value.id
  }
  return query
}

// 清除圈子搜索
const handleClearCircleSearch = () => {
  clearCircleSearch()
  // 如果当前在搜索页面，重新搜索（不带circle_id）
  if (route.name === 'search' && searchKeyword.value) {
    const keyword = searchKeyword.value.trim()
    if (keyword) {
      // 如果已经在搜索页面且搜索相同关键词，需要强制刷新
      if (route.query.q === keyword) {
        // 使用时间戳确保路由变化触发重新搜索
        router.replace({
          path: '/search',
          query: { q: keyword, t: Date.now() }
        })
      } else {
        router.push({
          path: '/search',
          query: { q: keyword }
        })
      }
    }
  }
}
</script>

<style scoped>
.app-header {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: var(--header-height);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  background: rgba(16, 16, 28, 0.8);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
  z-index: 1000;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
}

.header-left {
  display: flex;
  align-items: center;
}

.logo {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  user-select: none;
  text-decoration: none;
}

.logo-icon {
  width: 32px;
  height: 32px;
  flex-shrink: 0;
}

.logo-text {
  font-size: 1.5rem;
  font-weight: 700;
  background: rgba(96, 248, 187, 0.867);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
  background-clip: text;
}

.header-center {
  flex: 1;
  display: flex;
  justify-content: center;
}

/* 搜索包装器 */
.search-wrapper {
  position: relative;
  min-width: 40dvw;
}

/* 搜索栏 */
.search-box {
  width: 100%;
  display: flex;
  align-items: center;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 24px;
  padding: 6px 16px;
  min-height: 44px;
  transition: all 0.3s ease;
  gap: 8px;
}

.search-box:hover {
  background: rgba(255, 255, 255, 0.08);
  border: 2px rgba(83, 249, 166, 0.345) solid;
}

.search-box:focus-within {
  background: rgba(255, 255, 255, 0.1);
  border:2px rgba(83, 249, 166, 0.616) solid;
  box-shadow: 0 0 0 3px rgba(59, 223, 125, 0.152);
}

/* 圈子标签 */
.circle-tag {
  display: flex;
  align-items: center;
  gap: 6px;
  background: rgba(131, 248, 149, 0.264);
  border: 1px solid rgba(90, 246, 160, 0.379);
  border-radius: 16px;
  padding: 4px 8px 4px 4px;
  flex-shrink: 0;
}

.circle-tag-avatar {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  object-fit: cover;
  flex-shrink: 0;
}

.circle-tag-avatar-placeholder {
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 0.7rem;
  font-weight: bold;
  color: white;
  flex-shrink: 0;
}

.circle-tag-name {
  color: rgba(255, 255, 255, 0.9);
  font-size: 0.85rem;
  font-weight: 500;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.circle-tag-close {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  background: rgba(255, 255, 255, 0.1);
  border: none;
  border-radius: 50%;
  cursor: pointer;
  color: rgba(255, 255, 255, 0.6);
  transition: all 0.2s ease;
  padding: 0;
  flex-shrink: 0;
}

.circle-tag-close:hover {
  background: rgba(255, 255, 255, 0.2);
  color: rgba(255, 255, 255, 0.9);
}

.circle-tag-close svg {
  width: 10px;
  height: 10px;
}

.search-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  color: rgba(255, 255, 255, 0.5);
  flex-shrink: 0;
}

.search-input {
  flex: 1;
  background: transparent;
  border: none;
  outline: none;
  color: rgba(255, 255, 255, 0.9);
  font-size: 0.95rem;
  padding: 0;
  min-width: 0;
}

.search-input::placeholder {
  color: rgba(255, 255, 255, 0.4);
}

/* 搜索建议下拉菜单 */
.search-suggestions {
  position: absolute;
  top: calc(100% + 6px);
  left: 0;
  right: 0;
  background: rgba(30, 30, 46, 0.96);
  backdrop-filter: blur(20px);
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 12px;
  padding: 6px;
  z-index: 1001;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.3);
}

.suggestion-item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 14px;
  border-radius: 8px;
  cursor: pointer;
  text-decoration: none;
  transition: background 0.15s ease;
}

.suggestion-item:hover {
  background: rgba(59, 223, 125, 0.1);
}

.suggestion-icon {
  width: 16px;
  height: 16px;
  color: rgba(59, 223, 125, 0.7);
  flex-shrink: 0;
}

.suggestion-text {
  color: rgba(255, 255, 255, 0.85);
  font-size: 0.9rem;
}

.suggestion-fade-enter-active,
.suggestion-fade-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.suggestion-fade-enter-from,
.suggestion-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.header-right {
  display: flex;
  align-items: center;
  gap: 16px;
}

/* 匿名态「登录」按钮：覆盖下方全局透明按钮样式，给主题色 */
.login-btn {
  --n-color: rgba(102, 234, 194, 0.16) !important;
  --n-color-hover: rgba(102, 234, 194, 0.26) !important;
  --n-color-pressed: rgba(102, 234, 194, 0.34) !important;
  --n-border: 1px solid rgba(102, 234, 194, 0.5) !important;
  --n-border-hover: 1px solid rgba(102, 234, 194, 0.7) !important;
  --n-text-color: #66eac2 !important;
  --n-text-color-hover: #8af0d0 !important;
  border-radius: 20px !important;
  padding: 0 18px !important;
}

/* 消息中心 */
.notification-wrapper {
  position: relative;
  display: flex;
  align-items: center;
}

.notification-btn {
  color: rgba(255, 255, 255, 0.85);
  transition: all 0.3s ease;
}

.notification-btn:hover {
  color: #66ea92dd;
  background: rgba(102, 126, 234, 0.1) !important;
  border-radius: 32px;
}

.notification-badge {
  position: absolute;
  top: 4px;
  right: 4px;
  pointer-events: none;
}

.bell-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 20px;
  height: 20px;
}

/* 用户信息 */
.user-info-trigger {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 6px 6px 6px 6px;
  border-radius: 24px;
  cursor: pointer;
  transition: all 0.3s ease;
  /* background: rgba(255, 255, 255, 0.05); */
}

.user-info-trigger:hover {
  background: rgba(255, 255, 255, 0.1);
}

.user-avatar {
  /* background: linear-gradient(135deg, #ec4899 0%, #a855f7 50%, #3b82f6 100%); */
  color: white;
  font-weight: 600;
  /* box-shadow: 0 2px 8px rgba(236, 72, 153, 0.3); */
}

.username {
  color: rgba(255, 255, 255, 0.9);
  font-size: 0.95rem;
  font-weight: 500;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dropdown-icon {
  display: flex;
  align-items: center;
  color: rgba(255, 255, 255, 0.6);
  width: 16px;
  height: 16px;
  transition: transform 0.3s ease;
}

.user-info-trigger:hover .dropdown-icon {
  transform: rotate(180deg);
}

/* NaiveUI 组件样式覆盖 */
:deep(.n-button) {
  --n-color: transparent !important;
  --n-color-hover: rgba(102, 126, 234, 0.1) !important;
  --n-color-pressed: rgba(102, 126, 234, 0.15) !important;
  --n-border: none !important;
}

/* 下拉菜单的暗色玻璃主题样式已移至全局 main.css：
 * NDropdown 默认 render-to-body=true，菜单被传送到 <body> 下，
 * 此处的 scoped :deep() 无法命中，写在全局才能生效。 */

:deep(.n-badge) {
  --n-color: #f56c6c !important;
}
</style>
