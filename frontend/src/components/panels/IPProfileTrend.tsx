import { useEffect, useRef } from 'react'
import { useEchart } from '../../hooks/useEchart'
import { formatAxisTime, formatBytes } from '../../lib/format'
import type { IPProfileTrendPoint } from '../../api/types'

const chartBaseText = { color: '#8b93a0', fontFamily: 'ui-monospace, "SF Mono", "Cascadia Code", monospace', fontSize: 10.5 }

export function IPProfileTrend({ trend }: { trend: IPProfileTrendPoint[] }) {
  const divRef = useRef<HTMLDivElement>(null)
  const chartRef = useEchart(divRef)

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    const spanMs = trend.length > 1 ? new Date(trend[trend.length - 1].time).getTime() - new Date(trend[0].time).getTime() : 0
    chart.setOption(
      {
        backgroundColor: 'transparent',
        textStyle: chartBaseText,
        grid: { left: 10, right: 14, top: 12, bottom: 20, containLabel: true },
        tooltip: {
          trigger: 'axis',
          backgroundColor: '#131720',
          borderColor: '#3d4250',
          textStyle: { color: '#e2e6ea' },
          valueFormatter: (v: unknown) => formatBytes(v as number),
        },
        xAxis: {
          type: 'category',
          data: trend.map((p) => formatAxisTime(new Date(p.time).getTime(), spanMs)),
          axisLine: { lineStyle: { color: '#3d4250' } },
          axisLabel: { color: '#8b93a0' },
        },
        yAxis: {
          type: 'value',
          axisLabel: { color: '#8b93a0', formatter: (v: number) => formatBytes(v) },
          splitLine: { lineStyle: { color: '#232838' } },
        },
        series: [
          {
            type: 'line',
            smooth: true,
            symbol: 'none',
            areaStyle: { color: '#35e0ff', opacity: 0.28 },
            lineStyle: { width: 1.6, color: '#35e0ff' },
            data: trend.map((p) => p.bytes),
          },
        ],
      },
      false,
    )
  }, [chartRef, trend])

  return <div ref={divRef} style={{ width: '100%', height: 130 }} />
}
