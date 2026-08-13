import { useEffect, useRef, useState } from 'react'

const ROW_HEIGHT_PX = 39
const CHROME_PX = 44 + 39 + 40
const MIN_ROWS = 5
const RESIZE_SETTLE_MS = 400

export function useFitPageSize(ceiling: number, extraChromePx = 0) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [fit, setFit] = useState(Math.min(10, ceiling))

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const measure = () => {
      const available = el.clientHeight - CHROME_PX - extraChromePx
      const rows = Math.floor(available / ROW_HEIGHT_PX)
      setFit(Math.max(MIN_ROWS, Math.min(ceiling, rows || MIN_ROWS)))
    }
    measure()

    let settleTimer: ReturnType<typeof setTimeout> | undefined
    const ro = new ResizeObserver(() => {
      // A drag-resize (e.g. dragging the browser DevTools panel) fires
      // this many times per second; only commit once it settles, so we
      // don't re-fetch data on every intermediate size.
      if (settleTimer) clearTimeout(settleTimer)
      settleTimer = setTimeout(measure, RESIZE_SETTLE_MS)
    })
    ro.observe(el)
    return () => {
      if (settleTimer) clearTimeout(settleTimer)
      ro.disconnect()
    }
  }, [ceiling, extraChromePx])

  return { containerRef, fit }
}
