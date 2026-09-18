import { useEffect, useRef } from 'react'
import echarts, { guardZeroSizePaint } from '../lib/echarts'
import type { ECharts } from 'echarts/core'

export function WorldBackdrop() {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    let chart: ECharts | null = null
    let disposed = false
    fetch('/vendor/world.json')
      .then((r) => r.json())
      .then((geoJson) => {
        if (disposed || !ref.current) return
        echarts.registerMap('world', geoJson)
        chart = echarts.init(ref.current, null, { renderer: 'canvas' })
        guardZeroSizePaint(chart, ref.current)
        chart.setOption({
          backgroundColor: 'transparent',
          geo: {
            map: 'world',
            roam: false,
            silent: true,
            center: [10, 15],
            zoom: 1.15,
            itemStyle: {
              areaColor: 'rgba(79,208,192,0.035)',
              borderColor: 'rgba(201,163,91,0.22)',
              borderWidth: 0.6,
            },
            emphasis: { disabled: true },
          },
        })
      })
      .catch(() => {})
    const ro = new ResizeObserver(() => chart?.resize())
    ro.observe(el)
    return () => {
      disposed = true
      ro.disconnect()
      chart?.dispose()
    }
  }, [])
  return <div ref={ref} className="world-backdrop" aria-hidden="true" />
}
