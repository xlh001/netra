import { createContext, useContext, type ReactNode } from 'react'
import { useConfig as useConfigInternal } from '../hooks/useConfig'
import type { ConfigDTO } from '../api/types'

interface ConfigContextValue {
  config: ConfigDTO | null
  loading: boolean
  error: Error | null
  refetch: () => Promise<void>
  save: (dto: ConfigDTO) => Promise<ConfigDTO>
}

const ConfigContext = createContext<ConfigContextValue | null>(null)

export function ConfigProvider({ children }: { children: ReactNode }) {
  const value = useConfigInternal()
  return <ConfigContext.Provider value={value}>{children}</ConfigContext.Provider>
}

export function useConfigContext(): ConfigContextValue {
  const ctx = useContext(ConfigContext)
  if (!ctx) throw new Error('useConfigContext must be used within ConfigProvider')
  return ctx
}
