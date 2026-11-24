import { createI18n } from 'vue-i18n'
import zhCN from './zh-CN'
import enUS from './en-US'

const messages = {
  'zh-CN': zhCN,
  'en-US': enUS
}

// 从 localStorage 获取保存的语言设置，默认使用浏览器语言
const getLocale = () => {
  // 依次校验新旧键：优先有效的 qubar_locale，否则回退到有效的 quba_locale
  const qubarLocale = localStorage.getItem('qubar_locale')
  const qubaLocale = localStorage.getItem('quba_locale')
  const savedLocale = (qubarLocale && messages[qubarLocale])
    ? qubarLocale
    : (qubaLocale && messages[qubaLocale])
      ? qubaLocale
      : null

  if (savedLocale) {
    // 将选中的语言持久化到新键，再移除旧键
    localStorage.setItem('qubar_locale', savedLocale)
    localStorage.removeItem('quba_locale')
    return savedLocale
  }

  // 检测浏览器语言
  const browserLang = navigator.language || navigator.userLanguage
  if (browserLang.startsWith('zh')) {
    return 'zh-CN'
  }
  return 'en-US'
}

const i18n = createI18n({
  legacy: false, // 使用 Composition API 模式
  locale: getLocale(),
  fallbackLocale: 'zh-CN', // 回退语言
  messages,
  globalInjection: true, // 全局注入 $t
  missing: (locale, key) => {
    // 开发环境警告缺失的翻译
    if (import.meta.env.DEV) {
      console.warn(`[i18n] Missing translation: ${key} for locale: ${locale}`)
    }
    return key
  }
})

export default i18n
