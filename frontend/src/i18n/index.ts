import { createI18n } from 'vue-i18n'
import zhCN from './locales/zh-CN'
import enUS from './locales/en-US'

export type LocaleKey = 'zh-CN' | 'en-US'

export const locales: Record<LocaleKey, typeof zhCN> = {
  'zh-CN': zhCN,
  'en-US': enUS,
}

export const languageOptions: { value: LocaleKey; label: string }[] = [
  { value: 'zh-CN', label: '简体中文' },
  { value: 'en-US', label: 'English' },
]

const i18n = createI18n({
  legacy: false,
  locale: 'zh-CN',
  fallbackLocale: 'zh-CN',
  messages: locales,
})

export function setLocale(lang: string): LocaleKey {
  const l = (Object.keys(locales) as LocaleKey[]).includes(lang as LocaleKey) ? (lang as LocaleKey) : 'zh-CN'
  i18n.global.locale.value = l
  return l
}

export function currentLocale(): LocaleKey {
  return i18n.global.locale.value
}

export default i18n