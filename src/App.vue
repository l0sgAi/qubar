<template>
  <NConfigProvider :theme="darkTheme" :locale="naiveUILocale">
    <NGlobalStyle />
    <NMessageProvider>
      <NDialogProvider>
        <!-- Transition 常驻，动画名/时长按路由 meta.pageFade 动态决定：
             离开页或进入页任一为 pageFade 即用 out-in 淡入淡出（离开过渡页也有完整淡出），
             其余路由间切换 duration=0 等同直切，行为与原生一致。
             duration 显式声明换场时机（根节点无过渡属性，Vue 无法自动探测），
             动画本体只作用于各页 .main-content，见 main.css 的 .page-fade-* -->
        <router-view v-slot="{ Component, route }">
          <Transition
            :key="sessionVersion"
            :name="shouldPageFade(route) ? 'page-fade' : undefined"
            mode="out-in"
            :duration="shouldPageFade(route) ? { enter: 220, leave: 160 } : 0"
          >
            <!-- 移动端外框下不支持的页面（meta.mobile.supported=false）改渲染「暂不支持页」，URL 不变 -->
            <component
              :is="isUnsupportedOnMobile(route, mobileShell) ? MobileUnsupported : Component"
              :key="route.name"
            />
          </Transition>
        </router-view>
        <!-- 全局访客操作登录引导（写操作前置拦截，避免触发 401 硬跳转） -->
        <LoginPromptModal />
      </NDialogProvider>
    </NMessageProvider>
  </NConfigProvider>
</template>

<script setup>
import { computed, ref, provide, readonly, watch, watchEffect } from 'vue'
import { useRouter } from 'vue-router'
import { darkTheme, zhCN, enUS } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { NConfigProvider, NGlobalStyle, NMessageProvider, NDialogProvider } from 'naive-ui'
import LoginPromptModal from '@/components/auth/LoginPromptModal.vue'
import { stopUnreadNotice } from '@/composables/useUnreadNotice'
import { auth } from '@/utils/auth'
import { applyPageTitle } from '@/utils/pageTitle'
import MobileUnsupported from '@/views/MobileUnsupported.vue'
import { useMobileShell } from '@/composables/useAppShell'
import { isUnsupportedOnMobile } from '@/router/mobileMeta'

const router = useRouter()
const { locale } = useI18n()

// 移动端外框开关同步到 <html class="mobile-shell">：
// main.css 据此把 --header-height 换成移动顶栏高度（与 JS 外框严格同步，而非单纯按媒体查询）
const mobileShell = useMobileShell()
watchEffect(() => {
  document.documentElement.classList.toggle('mobile-shell', mobileShell.value)
})

// 圈子搜索状态
const circleSearchState = ref({
  id: null,
  name: '',
  avatarUrl: ''
})

// 设置圈子搜索状态
const setCircleSearch = (circle) => {
  circleSearchState.value = {
    id: circle.id,
    name: circle.name,
    avatarUrl: circle.avatar_url || ''
  }
}

// 清除圈子搜索状态
const clearCircleSearch = () => {
  circleSearchState.value = {
    id: null,
    name: '',
    avatarUrl: ''
  }
}

// 换号后让当前业务页重新读取该用户的数据。登录和 OAuth 回调自行完成跳转。
const sessionVersion = ref(0)
watch(() => auth.getToken(), () => {
  stopUnreadNotice()
  clearCircleSearch()
  const route = router.currentRoute.value
  if (route.name === 'login' || route.name === 'success') return
  if (route.meta.requiresAuth && !auth.isAuthenticated()) {
    router.replace({ name: 'login' })
  } else {
    sessionVersion.value++
  }
})

// 提供圈子搜索状态和方法
provide('circleSearchState', readonly(circleSearchState))
provide('setCircleSearch', setCircleSearch)
provide('clearCircleSearch', clearCircleSearch)

// 记录上一个路由的 pageFade：动画决策看出、入两侧，
// 使「离开过渡页 → 普通页」也播放淡出，而非生硬直切
const prevRoutePageFade = ref(false)
router.afterEach((_, from) => {
  prevRoutePageFade.value = !!from.meta.pageFade
})

const shouldPageFade = (route) => !!route.meta.pageFade || prevRoutePageFade.value

// 监听路由变化，在跳转到主页、热门、发现等页面时清除圈子搜索状态
watch(() => router.currentRoute.value.name, (newRouteName) => {
  // 定义需要清除圈子搜索状态的页面
  const pagesToClear = ['home', 'hot', 'discover']

  if (pagesToClear.includes(newRouteName)) {
    clearCircleSearch()
  }
}, { immediate: false })

// Naive UI 组件国际化
const naiveUILocale = computed(() => locale.value === 'zh-CN' ? zhCN : enUS)

// 语言切换时同步刷新浏览器标签页标题（路由切换时由 afterEach 处理）
watch(locale, () => {
  applyPageTitle(router.currentRoute.value)
})
</script>

<style>
/* 全局样式已在 assets/main.css 中定义 */
</style>
