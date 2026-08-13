import { createContext, useContext, useMemo, type ReactNode } from 'react'
import { STRINGS, type StringKey } from './strings'

interface I18nContextValue {
  t: (key: StringKey, vars?: Record<string, string | number>) => string
}

const I18nContext = createContext<I18nContextValue | null>(null)

export function I18nProvider({ children }: { children: ReactNode }) {
  const t = useMemo(() => {
    return (key: StringKey, vars?: Record<string, string | number>) => {
      let s: string = STRINGS[key]
      if (vars) {
        for (const [k, v] of Object.entries(vars)) {
          s = s.replace(`{${k}}`, String(v))
        }
      }
      return s
    }
  }, [])

  const value = useMemo(() => ({ t }), [t])

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
