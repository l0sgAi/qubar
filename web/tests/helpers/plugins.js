// 组件测试公共插件：最小 i18n（真实中文文案）+ 内存路由
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory } from 'vue-router'
import zhCN from '@/locales/zh-CN'

export const createTestI18n = () => createI18n({
  legacy: false,
  locale: 'zh-CN',
  messages: { 'zh-CN': zhCN }
})

const Stub = { render: () => null }

/** routes 为 [{ path, name?, meta? }]，组件统一替换为空 stub。 */
export const createTestRouter = (routes) => createRouter({
  history: createMemoryHistory(),
  routes: routes.map(r => ({ component: Stub, ...r }))
})
