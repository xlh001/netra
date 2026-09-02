import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { STRINGS_ZH, STRINGS_EN, type StringKey } from './strings'
import { getPublicLanguage } from '../api/client'

export type Language = 'zh' | 'en'

interface I18nContextValue {
  t: (key: StringKey, vars?: Record<string, string | number>) => string
  language: Language
  setLanguage: (lang: Language) => void
}

const I18nContext = createContext<I18nContextValue | null>(null)

export function I18nProvider({ children }: { children: ReactNode }) {
  const [language, setLanguage] = useState<Language>('zh')

  useEffect(() => {
    getPublicLanguage()
      .then((res) => {
        if (res.language === 'en') setLanguage('en')
      })
      .catch(() => {})
  }, [])

  const t = useMemo(() => {
    const table = language === 'en' ? STRINGS_EN : STRINGS_ZH
    return (key: StringKey, vars?: Record<string, string | number>) => {
      let s: string = table[key]
      if (vars) {
        for (const [k, v] of Object.entries(vars)) {
          s = s.replace(`{${k}}`, String(v))
        }
      }
      return s
    }
  }, [language])

  const value = useMemo(() => ({ t, language, setLanguage }), [t, language])

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>
}

export function useI18n(): I18nContextValue {
  const ctx = useContext(I18nContext)
  if (!ctx) throw new Error('useI18n must be used within I18nProvider')
  return ctx
}

export function useT(): I18nContextValue['t'] {
  return useI18n().t
}
