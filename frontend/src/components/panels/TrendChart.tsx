import { useEffect, useRef } from 'react'
import { Spin } from 'antd'
import { useT } from '../../i18n/context'
import { useEchart } from '../../hooks/useEchart'
import { formatAxisTime, formatBytes, protoColor } from '../../lib/format'
import type { Timeseries } from '../../api/types'

const chartBaseText = { color: '#8b93a0', fontFamily: 'ui-monospace, "SF Mono", "Cascadia Code", monospace', fontSize: 10.5 }

const PROTO_TOP_N = 8

export function TrendChart({ timeseries, loading }: { timeseries: Timeseries | null; loading?: boolean }) {
  const t = useT()
  const divRef = useRef<HTMLDivElement>(null)
  const chartRef = useEchart(divRef)

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    const points = timeseries?.points ?? []

    const totalsByProto = new Map<string, number>()
    points.forEach((p) => {
      for (const [proto, bytes] of Object.entries(p.bytes || {})) {
        totalsByProto.set(proto, (totalsByProto.get(proto) ?? 0) + bytes)
      }
    })
    const sortedProtos = Array.from(totalsByProto.entries()).sort((a, b) => b[1] - a[1])
    const topProtos = sortedProtos.slice(0, PROTO_TOP_N).map(([proto]) => proto)
    const restProtos = sortedProtos.slice(PROTO_TOP_N).map(([proto]) => proto)
    const protos = topProtos.slice().sort()

    const spanMs = points.length > 1 ? new Date(points[points.length - 1].time).getTime() - new Date(points[0].time).getTime() : 0
    const xData = points.map((p) => formatAxisTime(new Date(p.time).getTime(), spanMs))
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
    if (restProtos.length > 0) {
      series.push({
        name: t('protoOther'),
        type: 'line' as const,
        stack: 'total',
        smooth: true,
        symbol: 'none',
        areaStyle: { opacity: 0.55 },
        lineStyle: { width: 1.5, color: '#5c6b78' },
        itemStyle: { color: '#5c6b78' },
        emphasis: { focus: 'series' as const },
        data: points.map((p) => restProtos.reduce((sum, proto) => sum + (p.bytes?.[proto] ?? 0), 0)),
      })
    }
    const legendData = protos.map((p) => p.toUpperCase()).concat(restProtos.length > 0 ? [t('protoOther')] : [])

    chart.setOption(
      {
        backgroundColor: 'transparent',
        textStyle: chartBaseText,
        grid: { left: 10, right: 14, top: 26, bottom: 20, containLabel: true },
        legend: { data: legendData, top: 0, right: 0, textStyle: chartBaseText, itemWidth: 9, itemHeight: 9 },
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
      {loading && (
        <div className="panel-loading-overlay">
          <Spin size="small" />
        </div>
      )}
    </div>
  )
}
