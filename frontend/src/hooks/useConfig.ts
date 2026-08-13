import { useCallback, useEffect, useState } from 'react'
import { getConfig, putConfig } from '../api/client'
import type { ConfigDTO } from '../api/types'

export function useConfig() {
  const [config, setConfig] = useState<ConfigDTO | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)

  const refetch = useCallback(async () => {
    setLoading(true)
    try {
      const c = await getConfig()
      setConfig(c)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    refetch()
  }, [refetch])

  const save = useCallback(async (dto: ConfigDTO) => {
    const updated = await putConfig(dto)
    setConfig(updated)
    return updated
  }, [])

  return { config, loading, error, refetch, save }
}
