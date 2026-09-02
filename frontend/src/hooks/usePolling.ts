import { useCallback, useEffect, useRef, useState, type DependencyList } from 'react'

interface PollingState<T> {
  data: T | null
  error: Error | null
  loading: boolean
}

export function usePolling<T>(fetcher: () => Promise<T>, intervalMs: number, deps: DependencyList = []): PollingState<T> & { refetch: () => void } {
  const [state, setState] = useState<PollingState<T>>({ data: null, error: null, loading: true })
  const fetcherRef = useRef(fetcher)
  fetcherRef.current = fetcher
  const [refreshTick, setRefreshTick] = useState(0)

  useEffect(() => {
    let cancelled = false
    let timer: ReturnType<typeof setInterval> | undefined

    async function run() {
      try {
        const data = await fetcherRef.current()
        if (!cancelled) setState({ data, error: null, loading: false })
      } catch (err) {
        if (!cancelled) {
          setState((s) => ({ ...s, error: err instanceof Error ? err : new Error(String(err)), loading: false }))
        }
      }
    }

    setState((s) => ({ ...s, loading: true }))
    run()
    if (intervalMs > 0) {
      timer = setInterval(run, intervalMs)
    }

    return () => {
      cancelled = true
      if (timer) clearInterval(timer)
    }

  }, [intervalMs, refreshTick, ...deps])

  const refetch = useCallback(() => setRefreshTick((n) => n + 1), [])

  return { ...state, refetch }
}
