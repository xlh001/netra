import { useEffect, useRef, useState } from 'react'

export function useAnimatedNumber(value: number, formatter: (v: number) => string): string {
  const [display, setDisplay] = useState(() => formatter(value))
  const fromRef = useRef(value)

  useEffect(() => {
    const from = fromRef.current
    const to = value
    fromRef.current = value
    if (from === to) {
      setDisplay(formatter(to))
      return
    }

    let start: number | null = null
    const duration = 500
    let raf = 0

    function step(ts: number) {
      if (start === null) start = ts
      const progress = Math.min((ts - start) / duration, 1)
      const eased = 1 - Math.pow(1 - progress, 3)
      setDisplay(formatter(from + (to - from) * eased))
      if (progress < 1) raf = requestAnimationFrame(step)
    }
    raf = requestAnimationFrame(step)
    return () => cancelAnimationFrame(raf)

  }, [value])

  return display
}
