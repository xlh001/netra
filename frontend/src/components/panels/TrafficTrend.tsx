import { useEffect, useRef } from 'react'
import { Spin } from 'antd'
import { useT } from '../../i18n/context'
import echarts from '../../lib/echarts'
import { useEchart } from '../../hooks/useEchart'
import { useAnimatedNumber } from '../../hooks/useAnimatedNumber'
import { formatAxisTime, formatBps } from '../../lib/format'
import type { Timeseries } from '../../api/types'

function bucketSeconds(points: { time: string }[]): number {
  if (points.length < 2) return 0
  const span = new Date(points[points.length - 1].time).getTime() - new Date(points[0].time).getTime()
  return span > 0 ? span / 1000 / (points.length - 1) : 0
}

function MetricValue({ text }: { text: string }) {
  const match = text.match(/^([\d.,]+)\s*(.*)$/)
  return <strong><b>{match?.[1] ?? text}</b>{match?.[2] && <i>{match[2]}</i>}</strong>
}

export function TrafficTrend({ timeseries, loading }: { timeseries: Timeseries | null; loading?: boolean }) {
  const t = useT()
  const divRef = useRef<HTMLDivElement>(null)
  const chartRef = useEchart(divRef)

  const headPoints = timeseries?.points ?? []
  const headBucketSec = bucketSeconds(headPoints)
  const peakBps =
    headPoints.length && headBucketSec > 0
      ? (Math.max(...headPoints.map((p) => Object.values(p.bytes || {}).reduce((a, b) => a + b, 0))) * 8) / headBucketSec
      : 0
  const animatedPeakBps = useAnimatedNumber(peakBps, formatBps)

  useEffect(() => {
    const chart = chartRef.current
    if (!chart) return
    const points = timeseries?.points ?? []

    const spanMs = points.length > 1 ? new Date(points[points.length - 1].time).getTime() - new Date(points[0].time).getTime() : 0
    const bucketSec = bucketSeconds(points)
    const xData = points.map((p) => formatAxisTime(new Date(p.time).getTime(), spanMs))
    const totals = points.map((p) => {
      const bytes = Object.values(p.bytes || {}).reduce((a, b) => a + b, 0)
      return bucketSec > 0 ? (bytes * 8) / bucketSec : 0
    })
    const lastIdx = totals.length - 1

    const data = totals.map((v, i) =>
      i === lastIdx
        ? { value: v, symbol: 'circle', symbolSize: 6, itemStyle: { color: '#4FD0C0', borderColor: '#BFF5EE', borderWidth: 1.5 } }
        : v,
    )

    chart.setOption(
      {
        backgroundColor: 'transparent',
        grid: { left: 4, right: 8, top: 54, bottom: 4 },
        tooltip: {
          trigger: 'axis',
          backgroundColor: 'rgba(8,15,24,0.92)',
          borderColor: 'rgba(201,163,91,0.3)',
          textStyle: { color: '#ECE6D6' },
          formatter: (params: unknown) => {
            const p = (params as { axisValue: string; data: number | { value: number } }[])[0]
            const v = typeof p.data === 'object' ? p.data.value : p.data
            return `${p.axisValue}<br/>${formatBps(v)}`
          },
        },
        xAxis: {
          type: 'category',
          data: xData,
          boundaryGap: false,
          show: false,
        },
        yAxis: {
          type: 'value',
          show: false,
          min: 0,
        },
        series: [
          {
            type: 'line',
            smooth: true,
            showSymbol: false,
            lineStyle: { width: 1.8, color: '#4FD0C0', shadowColor: 'rgba(79,208,192,0.4)', shadowBlur: 6 },
            areaStyle: {
              color: new echarts.graphic.LinearGradient(0, 0, 0, 1, [
                { offset: 0, color: 'rgba(79,208,192,0.30)' },
                { offset: 1, color: 'rgba(79,208,192,0.02)' },
              ]),
            },
            emphasis: { disabled: true },
            data,
          },
        ],
      },
      true,
    )
  }, [chartRef, timeseries])

  return (
    <div className="panel trend-panel">
      <div className="panel-head">
        <h2>
          <span className="panel-head-title">{t('trendTitle')}</span>
        </h2>
      </div>
      <div className="chart-metric-stage">
        <div ref={divRef} className="trend-mini-canvas" />
        {peakBps > 0 && <div className="chart-metric chart-metric-peak"><span><i className="ticker-dot chart-metric-dot" />{t('peakLabel')}</span><MetricValue text={animatedPeakBps} /></div>}
      </div>
      {loading && (
        <div className="panel-loading-overlay">
          <Spin size="small" />
        </div>
      )}
    </div>
  )
}
