<template>
  <!-- 多根节点，只渲染外框（顶栏 + 侧栏 / TabBar），不包裹页面内容：
       桌面 DOM 与迁移前逐节点一致；页面内容不在本组件内，跨断点切换不会 remount。
       用法见 composables/useAppShell.js -->
  <template v-if="mobileShell">
    <MobileTopBar :mode="mobileMeta.topBar" :title="title">
      <template #actions><slot name="top-actions" /></template>
    </MobileTopBar>
    <MobileTabBar v-if="mobileMeta.tabBar" />
  </template>
  <template v-else>
    <AppHeader />
    <SideNav @collapsed="offset = SIDENAV_COLLAPSED_WIDTH" @expanded="offset = SIDENAV_WIDTH" />
  </template>
  <!-- 私信页以外收到新私信时弹提示（不渲染内容） -->
  <DmToastListener />
</template>

<script setup>
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppHeader from '@/components/layout/AppHeader.vue'
import SideNav from '@/components/layout/SideNav.vue'
import MobileTopBar from '@/components/layout/mobile/MobileTopBar.vue'
import MobileTabBar from '@/components/layout/mobile/MobileTabBar.vue'
import DmToastListener from '@/components/message/DmToastListener.vue'
import { useMobileShell, SIDENAV_WIDTH, SIDENAV_COLLAPSED_WIDTH } from '@/composables/useAppShell'
import { getMobileMeta } from '@/router/mobileMeta'

// 侧栏宽度回写给页面（页面据此计算内容区偏移）
const offset = defineModel('offset', { type: Number, default: SIDENAV_WIDTH })

const mobileShell = useMobileShell()
const route = useRoute()
const { t } = useI18n()

const mobileMeta = computed(() => getMobileMeta(route))

// back 顶栏标题：复用路由 titleKey（与标签页标题同源）
const title = computed(() => (route.meta.titleKey ? t(route.meta.titleKey) : ''))
</script>
