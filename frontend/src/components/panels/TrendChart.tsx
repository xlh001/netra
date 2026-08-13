import { useEffect, useRef } from 'react'
import { useT } from '../../i18n/context'
import { useEchart } from '../../hooks/useEchart'
import { formatBytes, protoColor } from '../../lib/format'
import type { Timeseries } from '../../api/types'

const chartBaseText = { color: '#8b93a0', fontFamily: 'ui-monospace, "SF Mono", "Cascadia Code", monospace', fontSize: 10.5 }

export function TrendChart({ timeseries }: { timeseries: Timeseries | null }) {
  const t = useT()
  const divRef = useRef<HTMLDivElement>(null)
  const chartRef = useEchart(divRef)

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    const points = timeseries?.points ?? []

    const protoSet = new Set<string>()
    points.forEach((p) => Object.keys(p.bytes || {}).forEach((k) => protoSet.add(k)))
    const protos = Array.from(protoSet).sort()

    const xData = points.map((p) => new Date(p.time).toLocaleTimeString('zh-CN', { hour12: false }))
    const series = protos.map((proto) => ({
      name: proto.toUpperCase(),
      type: 'line' as const,
      stack: 'total',
      smooth: true,
      symbol: 'none',
      areaStyle: { opacity: 0.55 },
      lineStyle: { width: 1.5, color: protoColor(proto) },
      itemStyle: { color: protoColor(proto) },
      emphasis: { focus: 'series' as const },
      data: points.map((p) => p.bytes?.[proto] ?? 0),
    }))

    chart.setOption(
      {
        backgroundColor: 'transparent',
        textStyle: chartBaseText,
        grid: { left: 10, right: 14, top: 26, bottom: 20, containLabel: true },
        legend: { data: protos.map((p) => p.toUpperCase()), top: 0, right: 0, textStyle: chartBaseText, itemWidth: 9, itemHeight: 9 },
        tooltip: {
          trigger: 'axis',
          backgroundColor: '#131720',
          borderColor: '#3d4250',
          textStyle: { color: '#e2e6ea' },
          valueFormatter: (v: unknown) => formatBytes(v as number),
        },
        xAxis: { type: 'category', data: xData, axisLine: { lineStyle: { color: '#3d4250' } }, axisLabel: { color: '#8b93a0' } },
        yAxis: {
          type: 'value',
          axisLabel: { color: '#8b93a0', formatter: (v: number) => formatBytes(v) },
          splitLine: { lineStyle: { color: '#232838' } },
        },
        series,
      },
      false,
    )
  }, [chartRef, timeseries])

  return (
    <div className="panel" style={{ flex: 0.7 }}>
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('trendTitle')}</span>
        </h2>
      </div>
      <div ref={divRef} id="trend-chart" />
    </div>
  )
}
