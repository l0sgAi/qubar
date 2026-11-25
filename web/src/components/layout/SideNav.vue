<template>
  <NLayout has-sider class="side-layout">
    <NLayoutSider
      bordered
      collapse-mode="width"
      :collapsed-width="64"
      :width="260"
      :collapsed="isCollapsed"
      show-trigger
      @collapse="whenCollapsedCLick"
      @expand="whenExpandCLick"
      class="custom-sider"
    >
      <!-- 导航菜单 -->
      <NMenu
        :collapsed="isCollapsed"
        :collapsed-width="64"
        :collapsed-icon-size="22"
        :options="menuOptions"
        :value="activeItem"
        @update:value="handleMenuSelect"
      />
    </NLayoutSider>

    <!-- 新建兴趣圈弹窗 -->
    <CreateCircleModal
      v-model:show="showCreateModal"
      @success="handleCreateSuccess"
    />
  </NLayout>
</template>

<script setup>
import { ref, h, computed, onMounted } from 'vue'
import { useRouter, useRoute, RouterLink } from 'vue-router'
import {
  NLayout,
  NLayoutSider,
  NMenu,
  NIcon,
  NAvatar
} from 'naive-ui'
import { Explore } from '@vicons/carbon'
import { RobotOutlined } from '@vicons/antd'
import { useI18n } from 'vue-i18n'
import CreateCircleModal from '@/components/circle/CreateCircleModal.vue'
import { useSideNavCircles } from '@/composables/useSideNavCircles'
import { usePendingInvites } from '@/composables/usePendingInvites'
import { Mail as MailIcon } from '@vicons/tabler'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()
const emit = defineEmits(['collapsed', 'expanded'])

const whenCollapsedCLick = () => {
  isCollapsed.value = !isCollapsed.value;
  emit('collapsed')
}
const whenExpandCLick = () => {
  isCollapsed.value = !isCollapsed.value;
  emit('expanded')
}
// 自定义 SVG 图标组件
const createIcon = (svgPath) => {
  return () => h('svg', {
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    'stroke-width': '2',
    style: { width: '20px', height: '20px' }
  }, svgPath.map(path => h('path', { d: path })))
}

// 主页图标
const HomeIcon = createIcon([
  'M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z',
  'M9 22V12h6v10'
])

// 热点图标
const FireIcon = createIcon([
  'M8.5 14.5A2.5 2.5 0 0 0 11 12c0-1.38-.5-2-1-3-1.072-2.143-.224-4.054 2-6 .5 2.5 2 4.9 4 6.5 2 1.6 3 3.5 3 5.5a7 7 0 1 1-14 0c0-1.153.433-2.294 1-3a2.5 2.5 0 0 0 2.5 2.5z'
])

// 新建图标
const PlusIcon = createIcon([
  'M12 5v14M5 12h14'
])

// 查看全部图标（更多 · 水平三点）
const MoreDotsIcon = () => h('svg', {
  viewBox: '0 0 24 24',
  fill: 'currentColor',
  style: { width: '20px', height: '20px' }
}, [
  h('circle', { cx: '5', cy: '12', r: '1.8' }),
  h('circle', { cx: '12', cy: '12', r: '1.8' }),
  h('circle', { cx: '19', cy: '12', r: '1.8' })
])

// 是否折叠
const isCollapsed = ref(false)

// 新建兴趣圈弹窗状态
const showCreateModal = ref(false)

// 我加入的 / 近期活跃 / 随机推荐圈子：数据缓存在模块级 composable 中（见
// useSideNavCircles），路由切换重新挂载本组件时复用缓存，不再每次发 3 个请求；
// 仅首次挂载、新建圈子成功、加入/退出圈子（广播事件）时才真正请求后端
const {
  joinedCircles,
  activeCircles,
  randomCircles,
  circlesLoading,
  activeCirclesLoading,
  randomCirclesLoading,
  ensureSideNavData,
  refreshJoinedCircles
} = useSideNavCircles()

// 待接受的圈子邀请：后端不发邮件也没有站内通知，有邀请时在主菜单显示带数量的入口（未登录时恒为 0）
const { pendingCount, refreshPendingInvites } = usePendingInvites()

onMounted(() => {
  ensureSideNavData(t('circle.interestCircle'))
  refreshPendingInvites()
})

const invitesMenuItem = computed(() => {
  const badge = pendingCount.value > 99 ? '99+' : String(pendingCount.value)
  return {
    label: () => h(RouterLink, { to: '/invites', class: 'nav-link nav-link--invites' }, {
      default: () => [
        h('span', null, t('circle.invite.mine.navEntry')),
        h('span', { class: 'nav-badge' }, badge)
      ]
    }),
    key: 'my-invites',
    icon: () => h('span', { class: 'nav-icon-wrap' }, [
      h(NIcon, null, { default: () => h(MailIcon) }),
      isCollapsed.value ? h('span', { class: 'nav-dot' }) : null
    ])
  }
})
const inviteItems = computed(() => (pendingCount.value > 0 ? [invitesMenuItem.value] : []))

// 机器人管理菜单项：对所有用户开放，后端做权限控制（避免异步角色判断导致菜单项跳动）
const agentMenuItem = computed(() => ({
  label: linkLabel('/admin/agents', t('agent.navEntry')),
  key: 'admin-agents',
  icon: () => h(NIcon, null, { default: () => h(RobotOutlined) })
}))

// 渲染兴趣圈图标：有头像显示头像，否则首字母 + 统一主题色背景
const renderCircleIcon = (circle) => {
  if (circle.avatar) {
    return h(NAvatar, {
      src: circle.avatar,
      size: 24,
      round: false,
      style: { width: '24px', height: '24px', borderRadius: '6px' }
    })
  }
  return h('div', {
    class: 'circle-icon',
    style: {
      width: '24px',
      height: '24px',
      borderRadius: '6px',
      background: 'color-mix(in srgb, var(--theme-color), #000 20%)',
      display: 'flex',
      alignItems: 'center',
      justifyContent: 'center',
      color: 'white',
      fontSize: '12px',
      fontWeight: 'bold'
    }
  }, (circle.name || '?').charAt(0))
}

// 骨架占位样式（加载中）
const skelStyle = (w, h, radius = '3px') => ({
  width: w,
  height: h,
  borderRadius: radius,
  background: 'linear-gradient(90deg, rgba(255,255,255,0.08) 25%, rgba(255,255,255,0.2) 37%, rgba(255,255,255,0.08) 63%)',
  backgroundSize: '400% 100%',
  animation: 's-nav-skel-shimmer 1.4s ease infinite'
})
// 骨架菜单项（复用于「我的圈子」「近期活跃」两组加载态）
const renderSkeletonItem = (key) => ({
  key,
  label: () => h('div', { style: skelStyle('110px', '14px') }),
  icon: () => h('div', { style: skelStyle('24px', '24px', '6px') }),
  disabled: true
})

// 导航项 label 用 RouterLink 渲染出真实 <a href>：
// 保留 hover 状态栏 URL、右键/中键新标签页；普通点击仍由 RouterLink 走 SPA 路由
const linkLabel = (to, text) => () =>
  h(RouterLink, { to, class: 'nav-link' }, { default: () => text })

// 当前激活的导航项：根据路由派生
// SideNav 在每个视图里独立挂载（非常驻布局），若用写死的 ref，
// 跳转后新挂载的实例会重置为默认值，导致高亮跟不上路由。
const activeItem = computed(() => {
  const path = route.path
  if (path === '/home') return 'home'
  if (path === '/hot') return 'hot'
  if (path === '/discover') return 'explore'
  if (path === '/admin/agents') return 'admin-agents'
  if (path === '/invites') return 'my-invites'
  if (path.startsWith('/circle/')) {
    const id = path.split('/')[2]
    // 匹配「我的圈子」或「近期活跃」中的对应条目
    if (joinedCircles.value.some(c => String(c.id) === id)) return `circle-${id}`
    if (activeCircles.value.some(c => String(c.id) === id)) return `active-circle-${id}`
    if (randomCircles.value.some(c => String(c.id) === id)) return `random-circle-${id}`
    return `circle-${id}`
  }
  if (path === '/profile' && route.query.tab === 'groups') return 'view-all-circles'
  return null
})

// 菜单选项
const menuOptions = computed(() => {
  // 折叠时只显示主要菜单项
  if (isCollapsed.value) {
    return [
      {
        label: linkLabel('/home', t('nav.home')),
        key: 'home',
        icon: () => h(NIcon, null, { default: () => h(HomeIcon) })
      },
      {
        label: linkLabel('/hot', t('nav.hot')),
        key: 'hot',
        icon: () => h(NIcon, null, { default: () => h(FireIcon) })
      },
      {
        label: linkLabel('/discover', t('nav.discover')),
        key: 'explore',
        icon: () => h(NIcon, null, { default: () => h(Explore) })
      },
      agentMenuItem.value,
      {
        label: t('circle.createCircle'),
        key: 'create',
        icon: () => h(NIcon, null, { default: () => h(PlusIcon) })
      },
      ...inviteItems.value
    ]
  }

  // 展开时显示完整菜单
  return [
    {
      label: linkLabel('/home', t('nav.home')),
      key: 'home',
      icon: () => h(NIcon, null, { default: () => h(HomeIcon) })
    },
    {
      label: linkLabel('/hot', t('nav.hot')),
      key: 'hot',
      icon: () => h(NIcon, null, { default: () => h(FireIcon) })
    },
    {
      label: linkLabel('/discover', t('nav.discover')),
      key: 'explore',
      icon: () => h(NIcon, null, { default: () => h(Explore) })
    },
    agentMenuItem.value,
    {
      label: t('circle.createCircle'),
      key: 'create',
      icon: () => h(NIcon, null, { default: () => h(PlusIcon) })
    },
    ...inviteItems.value,
    {
      type: 'divider',
      key: 'd1'
    },
    {
      type: 'group',
      key: 'joined-group',
      label: t('circle.myCircles'),
      children: circlesLoading.value
        ? Array.from({ length: 5 }, (_, i) => renderSkeletonItem(`joined-skeleton-${i}`))
        : [
            ...joinedCircles.value.map(circle => ({
              label: linkLabel(`/circle/${circle.id}`, circle.name),
              key: `circle-${circle.id}`,
              icon: () => renderCircleIcon(circle)
            })),
            {
              label: () => h(RouterLink, {
                to: { path: '/profile', query: { tab: 'groups' } },
                class: 'nav-link',
                style: { color: 'var(--theme-color)' }
              }, { default: () => t('circle.viewAll') }),
              key: 'view-all-circles',
              icon: () => h(NIcon, { color: 'var(--theme-color)' }, { default: () => h(MoreDotsIcon) })
            }
          ]
    },
    ...(activeCirclesLoading.value || activeCircles.value.length
      ? [
          { type: 'divider', key: 'd2' },
          {
            type: 'group',
            label: t('circle.active'),
            key: 'active-group',
            children: activeCirclesLoading.value
              ? Array.from({ length: 5 }, (_, i) => renderSkeletonItem(`active-skeleton-${i}`))
              : activeCircles.value.map(circle => ({
                  label: linkLabel(`/circle/${circle.id}`, circle.name),
                  key: `active-circle-${circle.id}`,
                  icon: () => renderCircleIcon(circle)
                }))
          }
        ]
      : []),
    ...(randomCirclesLoading.value || randomCircles.value.length
      ? [
          { type: 'divider', key: 'd3' },
          {
            type: 'group',
            label: t('circle.random'),
            key: 'random-group',
            children: randomCirclesLoading.value
              ? Array.from({ length: 5 }, (_, i) => renderSkeletonItem(`random-skeleton-${i}`))
              : randomCircles.value.map(circle => ({
                  label: linkLabel(`/circle/${circle.id}`, circle.name),
                  key: `random-circle-${circle.id}`,
                  icon: () => renderCircleIcon(circle)
                }))
          }
        ]
      : [])
  ]
})

// 菜单选择处理：label 已是 RouterLink，点击文字时这里与 RouterLink 各触发一次
// 跳转，vue-router 对同地址重复 push 会静默去重，无副作用；保留此 handler 是为了
// 让点击图标 / 空白区域（落在 <a> 外）仍能跳转
const handleMenuSelect = (key) => {
  if (key === 'create') {
    showCreateModal.value = true
  } else if (key === 'home') {
    router.push('/home')
  } else if (key === 'hot') {
    router.push('/hot')
  } else if (key === 'explore') {
    router.push('/discover')
  } else if (key === 'admin-agents') {
    router.push('/admin/agents')
  } else if (key === 'my-invites') {
    router.push('/invites')
  } else if (key === 'view-all-circles') {
    router.push({ path: '/profile', query: { tab: 'groups' } })
  } else if (key.startsWith('active-circle-')) {
    const circleId = key.replace('active-circle-', '')
    router.push(`/circle/${circleId}`)
  } else if (key.startsWith('random-circle-')) {
    const circleId = key.replace('random-circle-', '')
    router.push(`/circle/${circleId}`)
  } else if (key.startsWith('circle-')) {
    // 提取圈子 ID
    const circleId = key.replace('circle-', '')
    router.push(`/circle/${circleId}`)
  } else {
    console.log('Navigate to:', key)
  }
}

// 创建成功回调：新圈子即「我加入的圈子」，显式刷新侧栏该分区后跳转详情页
const handleCreateSuccess = (data) => {
  console.log('兴趣圈创建成功:', data)
  refreshJoinedCircles(t('circle.interestCircle'))
  if (data && data.id) {
    router.push(`/circle/${data.id}`)
  }
}
</script>

<style scoped>
.side-layout {
  position: fixed;
  left: 0;
  top: var(--header-height);
  bottom: 0;
  z-index: 999;
  overflow-y: hidden !important;
}

.custom-sider {
  background: rgba(24, 24, 35, 0.95) !important;
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  border-right: 1px solid rgba(255, 255, 255, 0.1) !important;
  height: calc(100vh - var(--header-height));
  height: calc(100dvh - var(--header-height));
  overflow-y: auto;
}

/* NLayoutSider 真正滚动的是内部 .n-layout-sider-scroll-container（自带 overflow:auto），
 * 滚动条长在它上面；隐藏需用 :deep 命中该内部元素，根元素上的 overflow 无效。 */
:deep(.n-layout-sider-scroll-container) {
  scrollbar-width: none; /* Firefox */
  -ms-overflow-style: none; /* IE/Edge 旧版 */
}

:deep(.n-layout-sider-scroll-container)::-webkit-scrollbar {
  display: none; /* Chrome/Safari */
}

/* NaiveUI Menu 组件样式覆盖 */
:deep(.n-menu) {
  background: transparent !important;
  color: rgba(255, 255, 255, 0.8) !important;
  padding: 12px;
}

:deep(.n-menu-item) {
  border-radius: 12px !important;
  margin: 4px 0 !important;
  color: rgba(255, 255, 255, 0.7) !important;
  transition: all 0.2s ease !important;
}

/* 选中态：不要自己叠方形背景（会和原生圆角层打架、且产生色差）。
 * Naive UI 的高亮是 .n-menu-item-content::before，带 border-radius 的圆角层，
 * 由一组 --n-item-* 变量驱动。直接喂主题绿变量给它，高亮自带圆角且配色统一。 */
:deep(.n-menu-item-content) {
  --n-border-radius: 10px !important;
  --n-item-color-hover: rgba(255, 255, 255, 0.06) !important;
  --n-item-color-active: rgba(102, 234, 194, 0.16) !important;
  --n-item-color-active-hover: rgba(102, 234, 194, 0.24) !important;
  --n-item-color-active-collapsed: rgba(102, 234, 194, 0.16) !important;
  --n-item-text-color-hover: rgba(255, 255, 255, 0.92) !important;
  --n-item-text-color-active: #8af0d0 !important;
  --n-item-text-color-active-hover: #8af0d0 !important;
  --n-item-icon-color-hover: rgba(255, 255, 255, 0.92) !important;
  --n-item-icon-color-active: #8af0d0 !important;
  --n-item-icon-color-active-hover: #8af0d0 !important;
}

:deep(.n-menu-item-content--selected) {
  font-weight: 600;
}

:deep(.n-menu-item-content) {
  display: flex !important;
  align-items: center !important;
  padding: 8px 12px !important;
}

:deep(.n-menu-item-content__icon) {
  color: inherit !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
}

:deep(.n-menu-item-content-header) {
  color: inherit !important;
}

/* RouterLink 渲染的菜单链接：跟随菜单配色，去掉默认下划线 */
:deep(.n-menu-item-content-header a.nav-link) {
  color: inherit;
  text-decoration: none;
}

/* 圈子邀请入口：文字 + 右侧数量角标；折叠时图标右上角小红点 */
:deep(a.nav-link--invites) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

:deep(.nav-badge) {
  min-width: 20px;
  padding: 0 6px;
  border-radius: 10px;
  background: rgba(255, 90, 90, 0.9);
  color: #fff;
  font-size: 0.72rem;
  font-weight: 600;
  line-height: 20px;
  text-align: center;
}

:deep(.nav-icon-wrap) {
  position: relative;
  display: inline-flex;
}

:deep(.nav-dot) {
  position: absolute;
  top: -2px;
  right: -3px;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #ff5a5a;
}

/* 折叠状态下的图标居中 */
:deep(.n-menu--collapsed .n-menu-item-content) {
  padding: 12px !important;
  justify-content: center !important;
}

/* 折叠态高亮条优化：侧栏仅 64px，菜单默认 padding:12px 会让高亮 ::before
 * （左右再各内缩 8px）挤得很窄。折叠时收窄菜单左右内边距、并把高亮条内缩改小，
 * 让圆角方块在图标周围舒展开，视觉更稳。 */
:deep(.n-menu--collapsed) {
  padding: 12px 8px !important;
}

:deep(.n-menu--collapsed .n-menu-item-content::before) {
  left: 4px !important;
  right: 4px !important;
}

:deep(.n-menu--collapsed .n-menu-item-content__icon) {
  margin-right: 0 !important;
}

/* 折叠时隐藏所有文字 */
:deep(.n-menu--collapsed .n-menu-item-content-header) {
  display: none !important;
}

/* 分组标题样式 */
:deep(.n-menu-group-title) {
  color: rgba(255, 255, 255, 0.5) !important;
  font-size: 0.75rem !important;
  font-weight: 600 !important;
  text-transform: uppercase !important;
  letter-spacing: 0.5px !important;
  padding: 12px 12px 8px 12px !important;
}

/* 折叠时隐藏分组标题和分组内的菜单项 */
:deep(.n-menu--collapsed .n-menu-group-title) {
  display: none !important;
}

:deep(.n-menu--collapsed .n-menu-group) {
  display: none !important;
}

/* 折叠时隐藏分割线 */
:deep(.n-menu--collapsed .n-divider) {
  display: none !important;
}

/* 分割线样式 */
:deep(.n-menu-item.n-menu-item--collapsed + .n-menu-item::before) {
  display: none;
}

:deep(.n-divider) {
  background: rgba(255, 255, 255, 0.1) !important;
  margin: 8px 0 !important;
}

/* 折叠触发器样式 */
:deep(.n-layout-sider-trigger) {
  background: rgba(255, 255, 255, 0.05) !important;
  border-top: 1px solid rgba(255, 255, 255, 0.1) !important;
  color: rgba(255, 255, 255, 0.6) !important;
  transition: all 0.3s ease !important;
}

:deep(.n-layout-sider-trigger:hover) {
  background: rgba(255, 255, 255, 0.1) !important;
  color: rgba(255, 255, 255, 0.9) !important;
}

/* 折叠按钮样式 */
:deep(.n-layout-toggle-button) {
  position: absolute !important;
  top: 50% !important;
  transform: translateY(-50%) !important;
  background: rgba(255, 255, 255, 0.1) !important;
  border: 1px solid rgba(255, 255, 255, 0.2) !important;
  color: rgba(255, 255, 255, 0.9) !important;
  width: 24px !important;
  height: 24px !important;
  border-radius: 50% !important;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
  cursor: pointer !important;
  transition: all 0.3s ease !important;
  z-index: 1000 !important;
}

/* Tooltip 样式调整 - 确保居中 */
:deep(.n-tooltip) {
  border-radius: 8px !important;
}

/* 兴趣圈图标动画 */
.circle-icon {
  transition: transform 0.2s ease;
  display: flex !important;
  align-items: center !important;
  justify-content: center !important;
}

:deep(.n-menu-item:hover .circle-icon) {
  transform: scale(1.1);
}

:deep(.n-layout-toggle-button){
  background-color: transparent !important;
  border: none !important;
}

</style>

<!-- 骨架 shimmer 关键帧（全局：骨架元素由 NMenu 内部 render，无 scoped data-v；尺寸/背景改 inline style，此处仅留 keyframes） -->
<style>
@keyframes s-nav-skel-shimmer {
  0% { background-position: 100% 50%; }
  100% { background-position: 0 50%; }
}
</style>
