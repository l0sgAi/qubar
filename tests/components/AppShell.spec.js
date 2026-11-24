import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, h, nextTick, onMounted, ref } from 'vue'
import { mount } from '@vue/test-utils'

const flags = vi.hoisted(() => ({ enabled: true }))
vi.mock('@/constants/breakpoints', async (importOriginal) => {
  const mod = await importOriginal()
  return {
    ...mod,
    get MOBILE_SHELL_ENABLED() { return flags.enabled }
  }
})

// 外框子组件替换为轻量 stub（真实组件依赖 naive-ui / 接口）
vi.mock('@/components/layout/AppHeader.vue', () => ({
  default: { name: 'AppHeader', render: () => h('header', { class: 'stub-header' }) }
}))
vi.mock('@/components/layout/SideNav.vue', () => ({
  default: {
    name: 'SideNav',
    emits: ['collapsed', 'expanded'],
    render: () => h('nav', { class: 'stub-sidenav' })
  }
}))
vi.mock('@/components/layout/mobile/MobileTopBar.vue', () => ({
  default: {
    name: 'MobileTopBar',
    props: ['mode', 'title'],
    setup: (props, { slots }) => () => h('div', { class: 'stub-topbar', 'data-mode': props.mode, 'data-title': props.title }, slots.actions?.())
  }
}))
vi.mock('@/components/layout/mobile/MobileTabBar.vue', () => ({
  default: { name: 'MobileTabBar', render: () => h('div', { class: 'stub-tabbar' }) }
}))
vi.mock('@/components/message/DmToastListener.vue', () => ({
  default: { name: 'DmToastListener', render: () => null }
}))

const { default: AppShell } = await import('@/components/layout/AppShell.vue')
const { useAppShell } = await import('@/composables/useAppShell')
const { __resetBreakpoint } = await import('@/composables/useBreakpoint')
const { setMatchMedia } = await import('../setup')
const { createTestI18n, createTestRouter } = await import('../helpers/plugins')

const ROUTES = [
  { path: '/home', name: 'home', meta: { titleKey: 'title.home' } },
  { path: '/post/:id', name: 'post-detail', meta: { titleKey: 'title.postDetail', mobile: { topBar: 'back', tabBar: false } } }
]

// 模拟迁移后的页面：<AppShell v-model:offset> + 兄弟内容节点
const mountPage = async (path = '/home', slots = {}) => {
  const router = createTestRouter(ROUTES)
  await router.push(path)
  const mounted = ref(0)
  const Content = defineComponent({
    setup() {
      onMounted(() => { mounted.value++ })
      return () => h('section', { class: 'page-content' })
    }
  })
  const Page = defineComponent({
    setup() {
      const { offset, mobileShell, contentStyle } = useAppShell()
      return () => h('div', { class: 'page' }, [
        h(AppShell, { offset: offset.value, 'onUpdate:offset': (v) => { offset.value = v } }, slots),
        h('div', { class: 'content-wrapper', style: contentStyle.value }, [h(Content)]),
        mobileShell.value ? null : h('aside', { class: 'right-sidebar' })
      ])
    }
  })
  const wrapper = mount(Page, { global: { plugins: [router, createTestI18n()] } })
  return { wrapper, mounted }
}

describe('AppShell', () => {
  beforeEach(() => {
    __resetBreakpoint()
    flags.enabled = true
  })

  it('桌面：渲染 AppHeader + SideNav，内容区让出侧栏宽度', async () => {
    const { wrapper } = await mountPage()
    expect(wrapper.find('.stub-header').exists()).toBe(true)
    expect(wrapper.find('.stub-sidenav').exists()).toBe(true)
    expect(wrapper.find('.stub-topbar').exists()).toBe(false)
    expect(wrapper.find('.content-wrapper').attributes('style')).toContain('margin-left: 260px')
  })

  it('不额外包裹 DOM：外框与内容是页面根的直接子节点', async () => {
    const { wrapper } = await mountPage()
    const children = Array.from(wrapper.element.children).map(el => el.className)
    expect(children).toEqual(['stub-header', 'stub-sidenav', 'content-wrapper', 'right-sidebar'])
  })

  it('SideNav 收起 / 展开回写 offset', async () => {
    const { wrapper } = await mountPage()
    const sideNav = wrapper.findComponent({ name: 'SideNav' })
    sideNav.vm.$emit('collapsed')
    await nextTick()
    expect(wrapper.find('.content-wrapper').attributes('style')).toContain('margin-left: 64px')
    sideNav.vm.$emit('expanded')
    await nextTick()
    expect(wrapper.find('.content-wrapper').attributes('style')).toContain('margin-left: 260px')
  })

  it('移动端：渲染 TopBar（main）+ TabBar，不渲染桌面外框与右栏，底部让出 TabBar', async () => {
    setMatchMedia(true)
    const { wrapper } = await mountPage('/home')
    expect(wrapper.find('.stub-header').exists()).toBe(false)
    expect(wrapper.find('.stub-sidenav').exists()).toBe(false)
    expect(wrapper.find('.right-sidebar').exists()).toBe(false)
    expect(wrapper.find('.stub-topbar').attributes('data-mode')).toBe('main')
    expect(wrapper.find('.stub-tabbar').exists()).toBe(true)
    const style = wrapper.find('.content-wrapper').attributes('style')
    expect(style).not.toContain('margin-left')
    expect(style).toContain('padding-bottom: calc(var(--tabbar-height) + var(--safe-bottom) + 16px)')
  })

  it('移动端按路由 meta：帖子详情为返回式顶栏、无 TabBar、标题取 titleKey', async () => {
    setMatchMedia(true)
    const { wrapper } = await mountPage('/post/1')
    const topBar = wrapper.find('.stub-topbar')
    expect(topBar.attributes('data-mode')).toBe('back')
    expect(topBar.attributes('data-title')).toBe('帖子详情')
    expect(wrapper.find('.stub-tabbar').exists()).toBe(false)
    expect(wrapper.find('.content-wrapper').attributes('style')).toContain('padding-bottom: calc(var(--safe-bottom) + 16px)')
  })

  it('#top-actions 插槽透传到移动顶栏', async () => {
    setMatchMedia(true)
    const { wrapper } = await mountPage('/post/1', { 'top-actions': () => h('button', { class: 'page-action' }) })
    expect(wrapper.find('.stub-topbar .page-action').exists()).toBe(true)
  })

  it('总开关关闭时手机断点仍渲染桌面外框', async () => {
    flags.enabled = false
    setMatchMedia(true)
    const { wrapper } = await mountPage()
    expect(wrapper.find('.stub-header').exists()).toBe(true)
    expect(wrapper.find('.stub-sidenav').exists()).toBe(true)
    expect(wrapper.find('.stub-topbar').exists()).toBe(false)
  })

  it('跨断点切换外框，页面内容不 remount', async () => {
    const { wrapper, mounted } = await mountPage()
    expect(mounted.value).toBe(1)

    setMatchMedia(true)
    await nextTick()
    expect(wrapper.find('.stub-header').exists()).toBe(false)
    expect(wrapper.find('.stub-topbar').exists()).toBe(true)

    setMatchMedia(false)
    await nextTick()
    expect(wrapper.find('.stub-header').exists()).toBe(true)
    expect(mounted.value).toBe(1)
  })
})
