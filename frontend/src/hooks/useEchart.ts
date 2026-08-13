import { useEffect, useRef, type RefObject } from 'react'
import type { ECharts } from 'echarts/core'
import echarts from '../lib/echarts'

export function useEchart(containerRef: RefObject<HTMLDivElement | null>): RefObject<ECharts | null> {
  const chartRef = useRef<ECharts | null>(null)

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    const chart = echarts.init(el, null, { renderer: 'canvas' })
    chartRef.current = chart
    const onResize = () => chart.resize()
    window.addEventListener('resize', onResize)
    return () => {
      window.removeEventListener('resize', onResize)
      chart.dispose()
      chartRef.current = null
    }

  }, [])

  return chartRef
}
