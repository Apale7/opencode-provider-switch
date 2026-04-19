import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import type { LanguagePreference } from '../types'
import en from './locales/en.json'
import zhCN from './locales/zh-CN.json'

export function normalizeSupportedLanguage(language?: string): 'en-US' | 'zh-CN' {
  const value = language?.trim().toLowerCase()
  if (value?.startsWith('zh')) {
    return 'zh-CN'
  }
  return 'en-US'
}

export function resolveLanguagePreference(language: LanguagePreference, systemLanguage?: string): 'en-US' | 'zh-CN' {
  if (language === 'en-US' || language === 'zh-CN') {
    return language
  }
  return normalizeSupportedLanguage(systemLanguage)
}

void i18n.use(initReactI18next).init({
  resources: {
    'en-US': { translation: en },
    'zh-CN': { translation: zhCN },
  },
  lng: 'en-US',
  fallbackLng: 'en-US',
  interpolation: {
    escapeValue: false,
  },
})

export default i18n
